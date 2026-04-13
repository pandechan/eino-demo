package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// =============================================================================
// 知识库文档 — 关于 Eino 框架的知识片段
// =============================================================================

var knowledgeDocs = []*schema.Document{
	{
		ID:      "eino-overview",
		Content: "Eino 是字节跳动 CloudWeGo 开源的 Go 语言 AI 应用开发框架。它提供组件抽象、图编排引擎、流式处理和 AOP 切面系统，帮助开发者构建高可用的 AI 应用。Eino 要求 Go 1.18 以上版本，使用 Apache-2.0 许可证。",
	},
	{
		ID:      "eino-components",
		Content: "Eino 定义了七大核心组件接口：ChatModel（LLM 对话）、Tool（工具调用）、Retriever（语义检索）、Embedding（文本向量化）、Indexer（向量索引存储）、Document Loader/Transformer/Parser（文档加载与处理）、ChatTemplate（提示词模板）。所有接口定义在核心仓库，具体实现在 eino-ext 仓库。",
	},
	{
		ID:      "eino-orchestration",
		Content: "Eino 的编排引擎提供三层 API：Chain（线性管道，最简单）、Graph（有向图，支持 DAG 和 Pregel 两种模式，可以有条件分支和循环）、Workflow（DAG 加字段级映射，控制最精细）。Graph 中使用 START 和 END 作为特殊节点，通过 AddEdge 连接节点，Compile 编译后得到 Runnable 可执行对象。",
	},
	{
		ID:      "eino-streaming",
		Content: "Eino 的流式处理是架构级设计。所有组件同时支持 Generate（同步）和 Stream（流式）两种调用模式。框架自动处理节点间流的拼接（concatenation）、装箱（boxing）、合并（merging）和复制（copying），开发者无需手动管理流转换。编译后的 Runnable 支持四种执行模式：Invoke、Stream、Collect、Transform。",
	},
	{
		ID:      "eino-agent",
		Content: "Eino 的 ADK（Agent Development Kit）提供多种预置 Agent：ChatModelAgent（最简单的入口，自动处理 ReAct 循环）、DeepAgent（复杂任务分解与子 Agent 委派）、Plan-Execute Agent（先规划后执行）、Supervisor Agent（多 Agent 监督协作）。支持 Interrupt/Resume 机制实现人工介入审批。",
	},
	{
		ID:      "eino-callback",
		Content: "Eino 的 AOP 切面系统提供五个生命周期切入点：OnStart（执行前）、OnEnd（成功后）、OnError（出错时）、OnStartWithStreamInput（流式输入）、OnEndWithStreamOutput（流式输出）。支持全局注册、运行时注入和 Graph 外使用三种方式。可对接 Langfuse、LangSmith、CozeLoop 等可观测平台。",
	},
	{
		ID:      "eino-models",
		Content: "Eino 通过 eino-ext 仓库支持 10 个 LLM：OpenAI、Claude、Gemini、Ark（字节火山引擎豆包）、Ollama、DeepSeek、Qwen（阿里通义千问）、Qianfan（百度千帆）、OpenRouter、ArkBot。Embedding 支持 OpenAI、Ark、Ollama、Gemini 等 8 个实现。",
	},
	{
		ID:      "eino-vectordb",
		Content: "Eino 支持 12 种向量数据库作为 Retriever/Indexer 后端：Elasticsearch 7/8/9、Milvus、Qdrant、Redis、OpenSearch 2/3、火山引擎 VikingDB。还支持 Dify 和火山 Knowledge 作为 Retriever。",
	},
	{
		ID:      "eino-tool-calling",
		Content: "Eino 的工具调用设计有 5 级接口：BaseTool（仅描述元信息）、InvokableTool（JSON string 输入输出）、StreamableTool（流式输出）、EnhancedInvokableTool（多模态结构化输入输出）、EnhancedStreamableTool（多模态流式）。推荐使用 ToolCallingChatModel 接口（WithTools 返回新实例），而非已废弃的 ChatModel.BindTools。",
	},
	{
		ID:      "eino-graph-detail",
		Content: "Graph 编排中，可通过 AddLambdaNode 添加自定义逻辑节点实现类型转换。AddGraphNode 支持子图嵌套。Graph 支持有状态模式：通过 WithGenLocalState 创建状态、WithStatePreHandler/WithStatePostHandler 在节点前后读写状态。Branch 实现条件分支，支持动态路由。Graph 编译后不可修改。",
	},
}

// =============================================================================
// 内存向量检索器 — 基于 Ollama Embedding 的余弦相似度搜索
// =============================================================================

// ollamaEmbeddingURL 是 Ollama Embedding 的 API 地址
var ollamaBaseURL = getEnvOrDefault("OLLAMA_HOST", "http://localhost:11434")
var embeddingModel = getEnvOrDefault("EMBEDDING_MODEL", "nomic-embed-text")

// MemoryRetriever 内存向量检索器
type MemoryRetriever struct {
	docs       []*schema.Document
	embeddings [][]float64 // 每个文档对应的向量
}

// NewMemoryRetriever 创建内存检索器并预计算文档向量
func NewMemoryRetriever(ctx context.Context, docs []*schema.Document) (*MemoryRetriever, error) {
	fmt.Println("📦 正在构建向量索引...")

	// 批量获取文档的 embedding
	texts := make([]string, len(docs))
	for i, doc := range docs {
		texts[i] = doc.Content
	}

	embeddings, err := getEmbeddings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("获取文档 embedding 失败: %w", err)
	}

	fmt.Printf("✅ 已索引 %d 个文档片段\n\n", len(docs))

	return &MemoryRetriever{
		docs:       docs,
		embeddings: embeddings,
	}, nil
}

// Retrieve 实现 retriever.Retriever 接口
func (r *MemoryRetriever) Retrieve(ctx context.Context, query string, opts ...interface{}) ([]*schema.Document, error) {
	// 获取 query 的 embedding
	queryEmbs, err := getEmbeddings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("获取 query embedding 失败: %w", err)
	}
	queryEmb := queryEmbs[0]

	// 计算余弦相似度并排序
	type scored struct {
		doc   *schema.Document
		score float64
	}

	var results []scored
	for i, docEmb := range r.embeddings {
		sim := cosineSimilarity(queryEmb, docEmb)
		results = append(results, scored{doc: r.docs[i], score: sim})
	}

	// 按相似度降序排序
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// 取 top-5，提高召回率
	topK := 5
	if topK > len(results) {
		topK = len(results)
	}

	out := make([]*schema.Document, topK)
	for i := 0; i < topK; i++ {
		out[i] = results[i].doc
		fmt.Printf("  🔍 检索到: [%.4f] %s\n", results[i].score, results[i].doc.ID)
	}

	return out, nil
}

// =============================================================================
// Ollama Embedding API 调用
// =============================================================================

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func getEmbeddings(ctx context.Context, texts []string) ([][]float64, error) {
	body := ollamaEmbedRequest{
		Model: embeddingModel,
		Input: texts,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, "POST", ollamaBaseURL+"/api/embed", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama Embed API 返回 %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 embedding 响应失败: %w", err)
	}

	return result.Embeddings, nil
}

// =============================================================================
// 数学工具
// =============================================================================

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// =============================================================================
// RAG Graph 编排
// =============================================================================

func buildRAGGraph(ctx context.Context) (compose.Runnable[string, *schema.Message], error) {
	// 1. 创建内存检索器
	retriever, err := NewMemoryRetriever(ctx, knowledgeDocs)
	if err != nil {
		return nil, err
	}

	// 2. 创建 ChatModel
	chatModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: ollamaBaseURL,
		Model:   getEnvOrDefault("OLLAMA_MODEL", "qwen2.5:3b"),
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ChatModel 失败: %w", err)
	}

	// 3. 创建 RAG Prompt 模板
	ragTemplate := prompt.FromMessages(schema.FString,
		&schema.Message{
			Role: schema.System,
			Content: `你是一个专业的技术助手。请基于以下参考资料回答用户的问题。
如果参考资料中没有相关信息，请如实说明。回答要简洁准确，使用中文。

参考资料:
{context}`,
		},
		&schema.Message{Role: schema.User, Content: "{question}"},
	)

	// 4. 构建有状态 Graph
	//
	//  ┌───────┐    ┌───────────┐    ┌──────────┐    ┌──────────┐    ┌─────┐
	//  │ START │───→│ retriever │───→│ formatter│───→│ template │───→│ llm │───→ END
	//  └───────┘    └───────────┘    └──────────┘    └──────────┘    └─────┘
	//     │                                              ↑
	//     │              query 通过 State 传递             │
	//     └──────────────────────────────────────────────┘
	//

	type ragState struct {
		Query string // 保存原始查询，供下游节点使用
	}

	g := compose.NewGraph[string, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *ragState {
			return &ragState{}
		}),
	)

	// 节点 1: retriever — 检索相关文档
	// input: string (query), output: []*schema.Document
	g.AddLambdaNode("retriever",
		compose.InvokableLambda(func(ctx context.Context, query string) ([]*schema.Document, error) {
			fmt.Printf("\n📖 正在检索与问题相关的文档...\n")
			return retriever.Retrieve(ctx, query)
		}),
		// 在 retriever 执行前，把 query 存入 state
		compose.WithStatePreHandler(func(ctx context.Context, query string, state *ragState) (string, error) {
			state.Query = query
			return query, nil
		}),
	)

	// 节点 2: formatter — 把检索结果格式化为 template 需要的 map
	// input: []*schema.Document, output: map[string]any
	g.AddLambdaNode("formatter",
		compose.InvokableLambda(func(ctx context.Context, docs []*schema.Document) (map[string]any, error) {
			// 拼接文档内容作为上下文
			var parts []string
			for i, doc := range docs {
				parts = append(parts, fmt.Sprintf("[%d] %s", i+1, doc.Content))
			}
			context := strings.Join(parts, "\n\n")

			return map[string]any{
				"context": context,
			}, nil
		}),
		// 从 state 读取 query 并注入到输出 map 中
		compose.WithStatePostHandler(func(ctx context.Context, out map[string]any, state *ragState) (map[string]any, error) {
			out["question"] = state.Query
			return out, nil
		}),
	)

	// 节点 3: template — 渲染 Prompt
	// input: map[string]any, output: []*schema.Message
	g.AddChatTemplateNode("template", ragTemplate)

	// 节点 4: llm — 生成回答
	// input: []*schema.Message, output: *schema.Message
	g.AddChatModelNode("llm", chatModel)

	// 连接边
	g.AddEdge(compose.START, "retriever")
	g.AddEdge("retriever", "formatter")
	g.AddEdge("formatter", "template")
	g.AddEdge("template", "llm")
	g.AddEdge("llm", compose.END)

	// 编译 Graph
	runnable, err := g.Compile(ctx, compose.WithGraphName("EinoRAG"))
	if err != nil {
		return nil, fmt.Errorf("编译 Graph 失败: %w", err)
	}

	return runnable, nil
}

// =============================================================================
// 主程序
// =============================================================================

func main() {
	ctx := context.Background()

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║   Eino RAG Demo — Graph 编排 + 向量检索       ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	// 构建 RAG Graph
	rag, err := buildRAGGraph(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "构建 RAG 失败: %v\n", err)
		os.Exit(1)
	}

	// 测试问题
	questions := []string{
		"Eino 支持哪些编排模式？",
		"Eino 的流式处理是怎么设计的？",
		"Eino 支持哪些大模型？",
	}

	// 如果有命令行参数，用它替换默认问题
	if len(os.Args) > 1 {
		questions = []string{strings.Join(os.Args[1:], " ")}
	}

	for i, q := range questions {
		if i > 0 {
			fmt.Println()
			fmt.Println("─────────────────────────────────────────────")
		}

		fmt.Printf("\n❓ 问题: %s\n", q)

		start := time.Now()

		// ===== 方式一：同步调用 =====
		resp, err := rag.Invoke(ctx, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "RAG 执行失败: %v\n", err)
			continue
		}

		fmt.Printf("\n💡 回答: %s\n", resp.Content)
		fmt.Printf("⏱  耗时: %v\n", time.Since(start))
	}

	// ===== 方式二：流式输出 =====
	fmt.Println()
	fmt.Println("═════════════════════════════════════════════")
	fmt.Println()

	streamQ := "Eino 的 Agent 系统有什么特点？"
	if len(os.Args) > 1 {
		streamQ = strings.Join(os.Args[1:], " ")
	}

	fmt.Printf("❓ 问题 (流式): %s\n", streamQ)

	start := time.Now()
	stream, err := rag.Stream(ctx, streamQ)
	if err != nil {
		fmt.Fprintf(os.Stderr, "RAG 流式执行失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Print("\n💡 回答: ")
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "\n读取流失败: %v\n", err)
			break
		}
		fmt.Print(chunk.Content)
	}
	fmt.Printf("\n⏱  耗时: %v\n", time.Since(start))
}

// =============================================================================
// 辅助函数
// =============================================================================

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
