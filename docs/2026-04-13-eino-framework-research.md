# Eino 框架研究报告

> 调研日期：2026-04-13
> 框架版本：基于 GitHub 最新 main 分支

---

## 一、框架概述

**Eino**（读音 ['aino]）是字节跳动 CloudWeGo 开源组织推出的 **Go 语言 AI 应用开发框架**，灵感来自 LangChain、LlamaIndex 和 Google ADK，但遵循 Go 语言编程惯例重新设计。

- **项目地址**：https://github.com/cloudwego/eino
- **扩展仓库**：https://github.com/cloudwego/eino-ext
- **许可证**：Apache-2.0
- **最低要求**：Go 1.18+（依赖泛型）

### 核心定位

填补 Go 生态在 LLM 应用开发框架上的空白。不是 LangChain 的 Go 移植，而是针对 Go 生态的特点（强类型、并发、简洁）重新设计的框架。

### 设计哲学

| 原则 | 体现 |
|------|------|
| **接口与实现分离** | 核心仓库只定义接口和编排逻辑，零外部依赖；具体实现全部在 eino-ext |
| **并发安全优先** | 废弃可变的 `ChatModel.BindTools()`，推荐不可变的 `ToolCallingChatModel.WithTools()` |
| **流式原生** | 所有组件同时支持同步和流式调用，框架自动处理流的拼接、合并、拷贝 |
| **类型安全** | 利用 Go 泛型，编排节点间有编译期类型检查 |
| **AOP 无侵入** | Callback 系统让可观测性与业务逻辑彻底解耦 |

---

## 二、整体架构

### 四层仓库结构

```
┌─────────────────────────────────────────────────┐
│                  eino-examples                   │  示例应用
├─────────────────────────────────────────────────┤
│                  eino-devops                     │  可视化调试、IDE 插件
├─────────────────────────────────────────────────┤
│                   eino-ext                       │  组件实现（LLM/向量库/工具/回调）
├─────────────────────────────────────────────────┤
│                    eino                          │  核心：接口、编排引擎、schema、AOP
└─────────────────────────────────────────────────┘
```

### 核心包结构（eino 仓库）

```
eino/
├── schema/              # 基础数据类型（Message, Document, Tool, StreamReader）
├── components/          # 组件接口定义（纯接口，无实现）
│   ├── model/           # ChatModel 接口
│   ├── embedding/       # Embedding 接口
│   ├── retriever/       # Retriever 接口
│   ├── indexer/         # Indexer 接口
│   ├── document/        # Loader, Transformer, Parser
│   ├── prompt/          # ChatTemplate
│   └── tool/            # BaseTool, InvokableTool, StreamableTool...
├── compose/             # 编排引擎（Graph/Chain/Workflow）
├── flow/                # 预置 Flow（ReAct Agent, MultiQuery Retriever 等）
├── adk/                 # Agent Development Kit（高级 Agent 框架）
├── callbacks/           # 回调/切面系统
└── internal/            # 内部工具
```

---

## 三、核心组件体系

组件是框架的"原子单元"，核心仓库只定义接口，具体实现在 eino-ext。

### 3.1 七大组件接口

| 组件 | 接口 | 职责 |
|------|------|------|
| **ChatModel** | `BaseChatModel` / `ToolCallingChatModel` | LLM 对话，`Generate()` 同步 + `Stream()` 流式 |
| **Tool** | `BaseTool` → `InvokableTool` → `EnhancedInvokableTool` | 工具调用（5 级接口，从简单到多模态流式） |
| **Retriever** | `Retriever` | 语义检索，`Retrieve(ctx, query)` → `[]*Document` |
| **Embedding** | `Embedder` | 文本向量化 |
| **Indexer** | `Indexer` | 向量索引写入 |
| **Document** | `Loader` / `Transformer` / `Parser` | 文档加载、处理、解析 |
| **Prompt** | `ChatTemplate` / `MessagesTemplate` | 提示词模板 |

### 3.2 关键数据结构

```go
// Message — 贯穿全框架的核心数据结构
type Message struct {
    Role         RoleType              // "system" | "user" | "assistant" | "tool"
    Content      string
    MultiContent []MessageInputPart    // 多模态：图片/音频/视频/文件
    ToolCalls    []ToolCall
    ToolCallID   string
    ResponseMeta *ResponseMeta         // TokenUsage, FinishReason
}

// 便捷构造
schema.SystemMessage("你是一个助手")
schema.UserMessage("你好")
schema.AssistantMessage("你好！有什么可以帮你的？")
schema.ToolMessage(result, toolCallID)
```

### 3.3 ChatModel 接口演进

```go
// ✅ 推荐：不可变，并发安全
type ToolCallingChatModel interface {
    BaseChatModel
    WithTools(tools []*schema.ToolInfo) (ToolCallingChatModel, error)  // 返回新实例
}

// ❌ 已废弃：可变，有竞态风险
type ChatModel interface {
    BaseChatModel
    BindTools(tools []*schema.ToolInfo) error  // 原地修改
}
```

### 3.4 Tool 接口分级

| 级别 | 接口 | 输入 → 输出 | 场景 |
|------|------|------------|------|
| 1 | `BaseTool` | — → `ToolInfo` | 仅描述元信息 |
| 2 | `InvokableTool` | JSON string → string | 最简单的同步调用 |
| 3 | `StreamableTool` | JSON string → `StreamReader[string]` | 流式输出 |
| 4 | `EnhancedInvokableTool` | `ToolArgument` → `ToolResult` | 多模态结构化 |
| 5 | `EnhancedStreamableTool` | `ToolArgument` → `StreamReader[ToolResult]` | 多模态流式 |

---

## 四、编排引擎

编排是 Eino 的核心能力——将组件组合成可执行的数据流。

### 4.1 三层编排 API

| 层级 | 适用场景 | 特点 |
|------|---------|------|
| **Chain** | 线性顺序执行 | Graph 的简化封装，最易上手 |
| **Graph** | 灵活有向图 | DAG（无环）或 Pregel（有环），支持条件分支和循环 |
| **Workflow** | DAG + 字段级映射 | 最精细的控制粒度 |

### 4.2 Graph 编排详解

```
┌───────┐    ┌─────┐    ┌─────────┐    ┌─────┐
│ START │───→│ LLM │───→│ Process │───→│ END │
└───────┘    └─────┘    └─────────┘    └─────┘

            DAG 模式（无环，AllPredecessor 触发）
```

```
┌───────┐    ┌───────┐    ┌───────┐    ┌─────┐
│ START │───→│ Agent │───→│ Tools │───→│ END │
└───────┘    └───────┘    └───────┘    └─────┘
                 ↑            │
                 └────────────┘   ← 循环

            Pregel 模式（有环，AnyPredecessor 触发）
```

**关键概念**：
- `START` / `END`：图的入口和出口特殊节点
- **类型化节点添加**：`AddChatModelNode()`, `AddRetrieverNode()`, `AddLambdaNode()`, `AddGraphNode()`（子图嵌套）
- **Branch**：条件分支，实现动态路由
- **State Graph**：有状态图，支持跨节点的状态读写（类 LangGraph 的 StateGraph）
- **Checkpoint**：检查点机制，支持 Interrupt/Resume（人工介入）
- **编译模型**：`graph.Compile()` → `Runnable`，编译后不可修改

### 4.3 Graph API 详解

#### 创建 Graph

```go
// 无状态 Graph
g := compose.NewGraph[string, string]()

// 有状态 Graph — 通过 WithGenLocalState 注入每次执行的局部状态
g := compose.NewGraph[string, *schema.Message](
    compose.WithGenLocalState(func(ctx context.Context) *MyState {
        return &MyState{}
    }),
)
```

#### 节点添加方法（Add*Node）

Graph 为每种组件提供类型化的节点添加方法，确保编译时类型安全：

```go
// 组件节点 — 输入输出类型由组件接口决定
g.AddChatModelNode(key, chatModel, opts...)            // []*Message → *Message
g.AddChatTemplateNode(key, template, opts...)           // map[string]any → []*Message
g.AddRetrieverNode(key, retriever, opts...)             // string → []*Document
g.AddEmbeddingNode(key, embedder, opts...)              // []string → [][]float64
g.AddIndexerNode(key, indexer, opts...)                 // []*Document → []string
g.AddLoaderNode(key, loader, opts...)                   // Source → []*Document
g.AddDocumentTransformerNode(key, transformer, opts...) // []*Document → []*Document
g.AddToolsNode(key, toolsNode, opts...)                 // []*Message → []*Message

// 自定义逻辑节点
g.AddLambdaNode(key, lambda, opts...)                   // 任意 I → O 类型转换
g.AddGraphNode(key, subGraph, opts...)                  // 嵌套子图
g.AddPassthroughNode(key, opts...)                      // 直通（Pregel 模式用）
```

#### Lambda — 类型桥接的万能胶水

**Lambda 是 Graph 编排中最重要的工具**，用于在组件之间做类型转换：

```go
// 同步 Lambda
compose.InvokableLambda(func(ctx context.Context, docs []*schema.Document) (map[string]any, error) {
    // 将检索结果转换为模板需要的 map
    return map[string]any{"context": formatDocs(docs)}, nil
})

// 流式 Lambda
compose.StreamableLambda(func(ctx context.Context, in string) (*schema.StreamReader[string], error) {
    // 返回流式输出
})

// 完整四合一（Invoke + Stream + Collect + Transform）
compose.AnyLambda[I, O, TOption](invoke, stream, collect, transform)
```

#### 边连接与分支

```go
// 顺序连接
g.AddEdge(compose.START, "nodeA")
g.AddEdge("nodeA", "nodeB")
g.AddEdge("nodeB", compose.END)

// 条件分支 — 根据运行时数据动态路由
branch := compose.NewGraphBranch(conditionFunc, map[string]bool{
    "pathA": true,
    "pathB": true,
})
g.AddBranch("nodeA", branch)
```

#### 编译与执行

```go
// 编译（编译后不可修改）
runnable, err := g.Compile(ctx, compose.WithGraphName("MyGraph"))

// 四种执行模式
result, err := runnable.Invoke(ctx, input)       // 同步
stream, err := runnable.Stream(ctx, input)        // 流式输出
result, err := runnable.Collect(ctx, streamIn)    // 流式输入
stream, err := runnable.Transform(ctx, streamIn)  // 流式输入 + 流式输出
```

### 4.4 有状态 Graph（State Graph）

State Graph 解决了编排中最常见的问题：**如何在不相邻的节点间传递数据**。

#### 核心 API

```go
// 1. 创建有状态 Graph
g := compose.NewGraph[string, *schema.Message](
    compose.WithGenLocalState(func(ctx context.Context) *MyState {
        return &MyState{}
    }),
)

// 2. 在节点执行前读/写状态
g.AddLambdaNode("node1", lambda,
    compose.WithStatePreHandler(func(ctx context.Context, input string, state *MyState) (string, error) {
        state.Query = input  // 将输入存入状态
        return input, nil    // 返回（可能修改后的）输入
    }),
)

// 3. 在节点执行后读/写状态
g.AddLambdaNode("node2", lambda,
    compose.WithStatePostHandler(func(ctx context.Context, output map[string]any, state *MyState) (map[string]any, error) {
        output["query"] = state.Query  // 从状态读取数据注入输出
        return output, nil
    }),
)
```

#### 典型场景

State Graph 在 RAG 中尤为关键：Retriever 消费了 query 输入，但下游的 ChatTemplate 也需要 query。通过 State 可以优雅地解决这个问题，无需破坏节点间的类型约束。

### 4.5 Chain API（线性流水线简化）

Chain 是 Graph 的简化封装，适合线性管道场景：

```go
chain := compose.NewChain[string, *schema.Message]()
chain.AppendRetriever(myRetriever)          // string → []*Document
chain.AppendLambda(formatDocsLambda)        // []*Document → map[string]any
chain.AppendChatTemplate(myTemplate)        // map[string]any → []*Message
chain.AppendChatModel(myModel)              // []*Message → *Message

runnable, err := chain.Compile(ctx)
```

支持的 Append 方法：`AppendChatModel`, `AppendChatTemplate`, `AppendRetriever`, `AppendEmbedding`, `AppendIndexer`, `AppendLoader`, `AppendDocumentTransformer`, `AppendLambda`, `AppendGraph`, `AppendBranch`, `AppendParallel`, `AppendPassthrough`

### 4.6 Prompt 模板系统

```go
// 使用 FString 模板语法（Python 风格 {variable}）
template := prompt.FromMessages(schema.FString,
    &schema.Message{Role: schema.System, Content: "基于以下上下文回答:\n{context}"},
    schema.MessagesPlaceholder("history", true),  // 可选的历史消息占位符
    &schema.Message{Role: schema.User, Content: "{question}"},
)

// 渲染
messages, err := template.Format(ctx, map[string]any{
    "context":  "...",
    "question": "...",
    "history":  []*schema.Message{...},  // 可选
})
```

支持三种模板语法：
- `schema.FString` — Python f-string 风格 `{variable}`
- `schema.GoTemplate` — Go text/template
- `schema.Jinja2` — Jinja2 模板 `{{variable}}`

### 4.7 四种执行模式

| 模式 | 输入 | 输出 | 场景 |
|------|------|------|------|
| **Invoke** | 普通值 | 普通值 | 标准同步调用 |
| **Stream** | 普通值 | `StreamReader` | 流式输出（打字机效果） |
| **Collect** | `StreamReader` | 普通值 | 收集流式输入 |
| **Transform** | `StreamReader` | `StreamReader` | 流式输入 + 流式输出 |

### 4.4 流自动处理

Eino 的一大亮点——开发者无需手动处理流转换，框架自动完成：
- **拼接**（concatenation）—— 流片段合并
- **装箱**（boxing）—— 类型转换
- **合并**（merging）—— 多流合并
- **复制**（copying）—— 一流多用

---

## 五、Agent 系统

### 5.1 ADK（Agent Development Kit）

ADK 是 Eino 的高层 Agent 框架，位于 `adk/` 包：

| Agent 类型 | 说明 |
|-----------|------|
| **ChatModelAgent** | 最简配置入门，自动处理 ReAct 循环 |
| **DeepAgent** | 复杂任务分解，子 Agent 委派 |
| **Plan-Execute Agent** | 先规划后执行模式 |
| **Supervisor Agent** | 多 Agent 监督协作 |
| **Workflow Agent** | 基于工作流的 Agent |

### 5.2 ReAct 循环

```
User Input
    ↓
┌──────────┐
│ ChatModel │ ←──────────────┐
└──────────┘                 │
    ↓                        │
 有 ToolCall?                │
    ├── 是 → 执行 Tool → 结果放回 Message 历史
    └── 否 → 返回最终回答
```

### 5.3 中间件系统

| 中间件 | 用途 |
|--------|------|
| `reduction` | 上下文长度压缩 |
| `summarization` | 对话摘要 |
| `plantask` | 任务计划与追踪 |
| `skill` | 技能注册与调度 |
| `filesystem` | 文件系统操作封装 |
| `dynamictool` | 动态工具搜索与选择 |

### 5.4 Human-in-the-Loop

- 任意 Agent 或 Tool 可暂停等待人工输入
- 通过 Checkpoint 持久化状态
- 支持 Interrupt → 人工审批 → Resume 完整流程

---

## 六、AOP / Callback 切面系统

### 6.1 五个生命周期切入点

| 时机 | 方法 | 触发条件 |
|------|------|---------|
| 开始前 | `OnStart` | 组件执行前（普通输入） |
| 成功后 | `OnEnd` | 组件执行成功后（普通输出） |
| 出错时 | `OnError` | 执行出错 |
| 流式输入 | `OnStartWithStreamInput` | 流式输入开始 |
| 流式输出 | `OnEndWithStreamOutput` | 流式输出结束 |

### 6.2 三种注入方式

| 方式 | 场景 | API |
|------|------|-----|
| **全局注册** | 进程级（初始化时） | `callbacks.AppendGlobalHandlers()` |
| **运行时注入** | 执行 Graph 时动态注入 | `compose.WithCallbacks()` + `DesignateNode()` |
| **Graph 外使用** | 不通过 Graph 编排 | `InitCallbacks()` |

### 6.3 设计约束

- Handler **不可修改** Input/Output（并发图执行中可能共享指针）
- 流式 Handler 收到的是 **拷贝的 StreamReader**，**必须关闭**避免 goroutine 泄漏
- Handler 间通信通过 `context.WithValue` 在不同时机间传递状态
- `TimingChecker` 可选接口，让 Handler 声明只关心哪些时机，跳过不需要的开销

---

## 七、集成生态（eino-ext）

### 7.1 LLM 模型（10 个）

| 实现 | 说明 |
|------|------|
| **OpenAI** | GPT 系列 |
| **Claude** | Anthropic |
| **Gemini** | Google |
| **Ark** | 字节火山引擎（豆包） |
| **ArkBot** | 火山引擎 Bot |
| **Ollama** | 本地模型 |
| **DeepSeek** | DeepSeek |
| **Qwen** | 阿里通义千问 |
| **Qianfan** | 百度千帆 |
| **OpenRouter** | 多模型路由 |

### 7.2 Embedding（8 个）

OpenAI, Ark, DashScope（阿里）, Gemini, Ollama, Qianfan, TencentCloud + Cache 缓存包装器

### 7.3 向量数据库

| 类型 | 支持 |
|------|------|
| **Indexer**（10 个） | ES 7/8/9, Milvus/Milvus2, OpenSearch 2/3, Qdrant, Redis, VikingDB |
| **Retriever**（12 个） | 以上全部 + Dify, 火山 Knowledge |

### 7.4 工具（10 个）

| 工具 | 用途 |
|------|------|
| Google Search | 搜索 |
| DuckDuckGo | 搜索 |
| Bing Search | 搜索 |
| SearXNG | 搜索引擎聚合 |
| Wikipedia | 百科查询 |
| HTTP Request | HTTP 请求 |
| Command Line | 命令行执行 |
| Browser Use | 浏览器自动化 |
| **MCP** | Model Context Protocol |
| Sequential Thinking | 序列化思考 |

### 7.5 文档处理

| 类型 | 支持 |
|------|------|
| **Loader** | File, S3, URL |
| **Parser** | HTML, PDF, DOCX, XLSX |
| **Splitter** | Recursive, Markdown Header, HTML Header, Semantic |
| **Transformer** | HTML Splitter, Score Reranker |

### 7.6 可观测性回调

| Handler | 说明 |
|---------|------|
| **Langfuse** | 开源 LLM 可观测平台 |
| **LangSmith** | LangChain 可观测平台 |
| **CozeLoop** | 字节 Coze 追踪 |
| **APMPlus** | 字节 APM 监控 |

---

## 八、典型使用模式

### 模式 1：简单 ChatModel 调用

```go
model, _ := openai.NewChatModel(ctx, config)
resp, _ := model.Generate(ctx, []*schema.Message{
    schema.UserMessage("你好"),
})
```

### 模式 2：ReAct Agent

```go
agent, _ := react.NewAgent(ctx, &react.AgentConfig{
    Model: model,
    Tools: tools,
    MaxSteps: 10,
})
resp, _ := agent.Generate(ctx, messages)
// 内部循环：Model → ToolCall → Tool → 回传 → Model → ... → 终止
```

### 模式 3：Graph 编排

```go
graph := compose.NewGraph[Input, Output]()
graph.AddChatModelNode("llm", model)
graph.AddLambdaNode("process", processFunc)
graph.AddEdge(START, "llm")
graph.AddEdge("llm", "process")
graph.AddEdge("process", END)

runnable, _ := graph.Compile()
result, _ := runnable.Invoke(ctx, input)
```

### 模式 4：Chain 线性编排

```go
chain := compose.NewChain[Input, Output]()
chain.AppendChatModel(model)
chain.AppendLambda(processFunc)

runnable, _ := chain.Compile()
result, _ := runnable.Invoke(ctx, input)
```

### 模式 5：State Graph（有环 Agent 循环）

```go
graph := compose.NewStateGraph[MyState](stateFactory)
graph.AddChatModelNode("agent", model, stateReader, stateWriter)
graph.AddToolNode("tools", toolNode)
graph.AddEdge(START, "agent")
graph.AddBranch("agent", branchFunc)  // 有 ToolCall → "tools"，否则 → END
graph.AddEdge("tools", "agent")       // 环：tools → agent

runnable, _ := graph.Compile()
```

### 模式 6：RAG Graph 编排（检索增强生成）

```go
// 定义跨节点共享的状态
type ragState struct {
    Query string
}

g := compose.NewGraph[string, *schema.Message](
    compose.WithGenLocalState(func(ctx context.Context) *ragState {
        return &ragState{}
    }),
)

// 节点 1: retriever — 检索相关文档
g.AddLambdaNode("retriever",
    compose.InvokableLambda(func(ctx context.Context, query string) ([]*schema.Document, error) {
        return myRetriever.Retrieve(ctx, query)
    }),
    // 执行前将 query 存入 state
    compose.WithStatePreHandler(func(ctx context.Context, query string, state *ragState) (string, error) {
        state.Query = query
        return query, nil
    }),
)

// 节点 2: formatter — 格式化检索结果为模板变量
g.AddLambdaNode("formatter",
    compose.InvokableLambda(func(ctx context.Context, docs []*schema.Document) (map[string]any, error) {
        var parts []string
        for i, doc := range docs {
            parts = append(parts, fmt.Sprintf("[%d] %s", i+1, doc.Content))
        }
        return map[string]any{"context": strings.Join(parts, "\n\n")}, nil
    }),
    // 执行后从 state 读取 query 注入输出
    compose.WithStatePostHandler(func(ctx context.Context, out map[string]any, state *ragState) (map[string]any, error) {
        out["question"] = state.Query
        return out, nil
    }),
)

// 节点 3: template — 渲染 RAG Prompt
ragTemplate := prompt.FromMessages(schema.FString,
    &schema.Message{Role: schema.System, Content: "基于以下参考资料回答问题:\n{context}"},
    &schema.Message{Role: schema.User, Content: "{question}"},
)
g.AddChatTemplateNode("template", ragTemplate)

// 节点 4: llm — 生成最终回答
g.AddChatModelNode("llm", chatModel)

// 连接边
g.AddEdge(compose.START, "retriever")
g.AddEdge("retriever", "formatter")
g.AddEdge("formatter", "template")
g.AddEdge("template", "llm")
g.AddEdge("llm", compose.END)

// 编译并执行
runnable, _ := g.Compile(ctx, compose.WithGraphName("EinoRAG"))
answer, _ := runnable.Invoke(ctx, "Eino 支持哪些编排模式？")   // 同步
stream, _ := runnable.Stream(ctx, "Eino 的流式处理怎么设计的？") // 流式
```

---

## 九、RAG 实战：Graph 编排深度解析

### 9.1 RAG 的两条流水线

**索引流水线（离线/预处理）**

```
Loader → Transformer(分割) → Embedder(向量化) → Indexer(存储)
```

类型流：`Source → []*Document → []*Document → [][]float64 → []string`

**查询流水线（在线/实时）**

```
Query → Retriever(检索) → Formatter(格式化) → ChatTemplate(渲染) → ChatModel(生成) → Answer
```

类型流：`string → []*Document → map[string]any → []*Message → *Message`

### 9.2 Graph 编排 RAG 的架构

```
┌───────┐    ┌───────────┐    ┌──────────┐    ┌──────────┐    ┌─────┐    ┌─────┐
│ START │───→│ retriever │───→│ formatter│───→│ template │───→│ llm │───→│ END │
└───────┘    └───────────┘    └──────────┘    └──────────┘    └─────┘    └─────┘
   │              ↑ ↓                              ↑
   │     State: 存 Query                  State: 读 Query
   └────────── WithGenLocalState 跨节点传递 ─────────┘
```

**为什么需要 State？**

RAG 的核心挑战在于：Retriever 节点消费了 `query` 作为输入，但下游的 ChatTemplate 也需要 `query`。由于 Graph 的数据是单向流动的（每个节点只接收上游节点的输出），`query` 在经过 Retriever 后就"丢失"了。

State Graph 通过 `WithGenLocalState` + `WithStatePreHandler/PostHandler` 优雅地解决了这个问题：
1. **Retriever 的 StatePreHandler**：执行前将 `query` 存入 `state.Query`
2. **Formatter 的 StatePostHandler**：执行后从 `state.Query` 读取并注入到输出 `map` 中

这样既保持了节点间严格的类型约束，又实现了跨节点数据传递。

### 9.3 Lambda 在 RAG 中的关键作用

RAG 流水线中，组件输出类型和下一个组件输入类型通常不匹配：

| 上游输出 | 下游输入 | 需要 Lambda |
|---------|---------|------------|
| Retriever → `[]*Document` | ChatTemplate → `map[string]any` | ✅ 文档列表 → 模板变量 |
| ChatTemplate → `[]*Message` | ChatModel → `[]*Message` | ❌ 类型匹配，直连 |

Lambda 是 Graph 编排中的"万能胶水"，任何类型不匹配的地方都用 `InvokableLambda` 桥接。

### 9.4 Retriever 接口与实现策略

```go
// Retriever 接口 — 仅一个方法
type Retriever interface {
    Retrieve(ctx context.Context, query string, opts ...Option) ([]*schema.Document, error)
}

// Retriever Options
retriever.WithTopK(5)                    // 返回 top-K 结果
retriever.WithScoreThreshold(0.5)        // 分数过滤阈值
retriever.WithEmbedding(embedder)        // 注入 Embedder
```

**实现策略**：

| 策略 | 适用场景 | 依赖 |
|------|---------|------|
| **自实现内存检索** | Demo/原型/小数据量 | 仅需 Embedding API |
| **eino-ext 后端** | 生产环境 | Redis/Milvus/Qdrant/ES 等 |
| **LLM 打分替代** | 无向量库时 | 仅需 ChatModel |

**内存向量检索的实现要点**：
1. 预处理阶段：调用 Embedding API 将所有文档转为向量
2. 检索阶段：将 query 转为向量，计算与所有文档向量的余弦相似度
3. 排序返回 top-K 结果

### 9.5 高级检索模式（flow/retriever/）

Eino 内置了三种增强检索模式：

| 模式 | 说明 | 适用场景 |
|------|------|---------|
| **MultiQuery** | 用 LLM 将原始 query 改写为多个变体，分别检索后去重融合 | 提高召回率 |
| **Parent** | 子文档检索命中后返回父文档（更大上下文） | 长文档场景 |
| **Router** | 根据 query 特征路由到不同 Retriever | 多知识库场景 |

```go
// MultiQuery Retriever 示例
multiRetriever := multiquery.NewRetriever(ctx, &multiquery.Config{
    RewriteLLM:    chatModel,           // 用于改写 query 的 LLM
    OrigRetriever: baseRetriever,       // 底层检索器
    MaxQueriesNum: 5,                   // 最多改写为 5 个变体
})
docs, _ := multiRetriever.Retrieve(ctx, "Eino 怎么做流式处理？")
```

### 9.6 RAG 实战经验总结

| 要点 | 说明 |
|------|------|
| **State 传递 Query** | 用 `WithGenLocalState` + `StatePreHandler/PostHandler` 跨节点传递原始问题 |
| **Lambda 桥接类型** | `[]*Document → map[string]any` 是 RAG 中最常见的 Lambda 用途 |
| **Embedding 模型选择** | 中文场景推荐使用中文优化的 Embedding 模型，nomic-embed-text 等英文模型对中文语义匹配效果有限 |
| **Top-K 调优** | top-K 太小可能漏掉相关文档，太大会引入噪音。建议从 3-5 开始调整 |
| **Graph vs Chain** | 简单线性 RAG 用 Chain 更简洁；需要状态传递、条件分支时用 Graph |
| **Compile 一次执行多次** | Graph 编译后得到的 Runnable 可重复使用，支持 Invoke 和 Stream 两种模式 |

---

## 十、Flow 增强检索模式

Eino 在 `flow/retriever/` 包下内置了高级检索模式，无需自己用 Graph 拼装：

| 模式 | 包路径 | 原理 |
|------|--------|------|
| `multiquery` | `flow/retriever/multiquery` | LLM 将 query 改写为多个变体 → 分别检索 → 去重融合，提升召回率 |
| `parent` | `flow/retriever/parent` | 按小 chunk 检索命中 → 返回所在的父文档（更大上下文窗口） |
| `router` | `flow/retriever/router` | 根据 query 特征路由到不同 Retriever（多知识库场景） |

同样，`flow/indexer/parent` 提供了对应的父子文档索引写入能力。

---

## 十一、学习路径

官方提供 10 章渐进式教程（eino-examples/quickstart）：

| 章节 | 主题 |
|------|------|
| ch01 | ChatModel & Message 基础 |
| ch02 | Agent & Runner 多轮对话 |
| ch03 | Memory & Session 持久化 |
| ch04 | Tool 集成与文件系统访问 |
| ch05 | Middleware 模式 |
| ch06 | Callback & 可观测性追踪 |
| ch07 | Interrupt/Resume 中断恢复 |
| ch08 | Graph Tool 复杂工作流 |
| ch09 | Skill 复用模式 |
| ch10 | A2UI 协议（流式 UI 渲染） |

---

## 十二、核心竞争力总结

| 维度 | 优势 |
|------|------|
| **Go 生态唯一** | 唯一成熟的 Go LLM 应用框架，填补生态空白 |
| **类型安全** | Go 泛型驱动，编译时检查节点间类型对齐 |
| **流式原生** | 不是事后补丁，而是架构底层设计的 StreamReader 机制 |
| **并发安全** | `WithTools()` 不可变模式、Callback 只读约束，处处考虑 goroutine 安全 |
| **AOP 解耦** | 5 种回调时机 + 3 种注入方式，横切关注点与业务逻辑彻底分离 |
| **丰富集成** | 10 个 LLM + 12 个向量库 + 10 个工具，覆盖国内外主流服务 |
| **字节系深度整合** | Ark/VikingDB/CozeLoop/APMPlus 等字节内部服务的一等支持 |
| **Human-in-the-Loop** | Checkpoint + Interrupt/Resume，企业级 Agent 场景必备 |
| **核心/扩展分离** | 核心仓库零外部依赖，扩展按需引入，依赖树干净 |
| **DevOps 工具链** | 可视化编排、调试工具，降低开发门槛 |

---

## 十三、与 LangChain(Go) 的定位差异

| 维度 | Eino | LangChain(Go) / LangChainGo |
|------|------|------|
| **维护方** | 字节跳动 CloudWeGo | 社区（tmc/langchaingo） |
| **设计风格** | Go 原生设计，泛型驱动 | Python LangChain 的 Go 移植 |
| **编排能力** | Chain + Graph(DAG/Pregel) + Workflow 三层 | 主要是 Chain |
| **Agent 系统** | ADK 完整框架（ReAct/Deep/PlanExecute/Supervisor） | 基础 Agent |
| **流处理** | 架构级自动流处理 | 基础流支持 |
| **Checkpoint** | 内置 Interrupt/Resume | 无 |
| **国内服务集成** | 火山引擎/千问/千帆/VikingDB 等深度集成 | 有限 |
| **企业背景** | 字节内部大规模验证 | 社区驱动 |

---

## 十四、适用场景建议

| 场景 | 推荐度 | 说明 |
|------|--------|------|
| Go 技术栈的 AI 应用 | ⭐⭐⭐⭐⭐ | 当前最佳选择 |
| RAG 检索增强生成 | ⭐⭐⭐⭐⭐ | 完整的 Loader→Embedding→Index→Retrieve 链路 |
| 复杂 Agent 编排 | ⭐⭐⭐⭐⭐ | State Graph + ReAct + Multi-Agent |
| 需要人工审批的 Agent | ⭐⭐⭐⭐⭐ | Checkpoint + Interrupt/Resume |
| 字节系基础设施 | ⭐⭐⭐⭐⭐ | Ark/VikingDB/CozeLoop 一等支持 |
| 简单的 LLM 调用 | ⭐⭐⭐ | 框架稍重，简单场景可直接用 SDK |
| Python 生态深度集成 | ⭐⭐ | Go 生态，与 Python ML 工具链集成需额外工作 |
