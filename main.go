package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "chat":
		runChat(ctx)
	case "stream":
		runStream(ctx)
	case "agent":
		runAgent(ctx)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Eino Demo — 基于 Ollama 本地模型")
	fmt.Println()
	fmt.Println("用法: go run . <command> [问题]")
	fmt.Println()
	fmt.Println("命令:")
	fmt.Println("  chat   [问题]   基础对话（同步生成）")
	fmt.Println("  stream [问题]   流式输出（打字机效果）")
	fmt.Println("  agent  [问题]   工具调用 Agent（ReAct 循环）")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  go run . chat 什么是微服务架构？")
	fmt.Println("  go run . stream 用Go写一个快排")
	fmt.Println("  go run . agent 现在几点了？帮我算下 3.14 * 100")
}

// =============================================================================
// Demo 1: 基础对话 — ChatModel.Generate
// =============================================================================

func runChat(ctx context.Context) {
	fmt.Println("=== Demo 1: 基础对话 ===")
	fmt.Println()

	chatModel := mustCreateModel(ctx)

	question := getUserQuestion("用一句话解释什么是 Go 语言的 goroutine？")
	messages := []*schema.Message{
		schema.SystemMessage("你是一个友好的助手，回答简洁明了。"),
		schema.UserMessage(question),
	}

	fmt.Printf("📤 发送: %s\n\n", question)

	start := time.Now()
	resp, err := chatModel.Generate(ctx, messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📥 回复: %s\n", resp.Content)
	fmt.Printf("\n⏱  耗时: %v\n", time.Since(start))
}

// =============================================================================
// Demo 2: 流式输出 — ChatModel.Stream
// =============================================================================

func runStream(ctx context.Context) {
	fmt.Println("=== Demo 2: 流式输出 ===")
	fmt.Println()

	chatModel := mustCreateModel(ctx)

	question := getUserQuestion("简要介绍 Eino 框架的三个核心特点，每个特点用一句话概括。")
	messages := []*schema.Message{
		schema.SystemMessage("你是一个技术专家，回答简洁。"),
		schema.UserMessage(question),
	}

	fmt.Printf("📤 发送: %s\n\n", question)
	fmt.Print("📥 回复: ")

	start := time.Now()
	stream, err := chatModel.Stream(ctx, messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "流式生成失败: %v\n", err)
		os.Exit(1)
	}

	// 逐 chunk 读取并打印，实现打字机效果
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "\n读取流失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(chunk.Content)
	}

	fmt.Printf("\n\n⏱  耗时: %v\n", time.Since(start))
}

// =============================================================================
// Demo 3: 工具调用 Agent — ReAct 循环
// =============================================================================

func runAgent(ctx context.Context) {
	fmt.Println("=== Demo 3: 工具调用 Agent ===")
	fmt.Println()

	chatModel := mustCreateModel(ctx)

	// 定义工具
	calcTool := &calculatorTool{}
	timeTool := &currentTimeTool{}

	// 创建 ReAct Agent
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{calcTool, timeTool},
		},
		MaxStep: 5,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建 Agent 失败: %v\n", err)
		os.Exit(1)
	}

	question := getUserQuestion("现在几点了？另外帮我算一下 sqrt(144) + 3.14 * 2 等于多少？")
	messages := []*schema.Message{
		schema.SystemMessage("你是一个有用的助手，可以使用工具来回答问题。请用中文回答。"),
		schema.UserMessage(question),
	}

	fmt.Printf("📤 发送: %s\n\n", question)

	start := time.Now()
	resp, err := agent.Generate(ctx, messages)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agent 执行失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📥 回复: %s\n", resp.Content)
	fmt.Printf("\n⏱  耗时: %v\n", time.Since(start))
}

// =============================================================================
// 工具实现
// =============================================================================

// --- 计算器工具 ---

type calculatorTool struct{}

func (t *calculatorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "calculator",
		Desc: "数学计算器，支持加减乘除和开方。输入一个数学表达式，返回计算结果。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"expression": {
				Type:     schema.String,
				Desc:     "数学表达式，如 '2 + 3'、'sqrt(16)'、'3.14 * 2'",
				Required: true,
			},
		}),
	}, nil
}

func (t *calculatorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	result := evalSimpleExpr(args.Expression)
	return fmt.Sprintf("计算结果: %s = %s", args.Expression, result), nil
}

// evalSimpleExpr 简单表达式求值（支持四则运算和 sqrt）
func evalSimpleExpr(expr string) string {
	expr = strings.TrimSpace(expr)

	// 处理 sqrt(x)
	if strings.HasPrefix(expr, "sqrt(") && strings.HasSuffix(expr, ")") {
		inner := expr[5 : len(expr)-1]
		val, err := strconv.ParseFloat(strings.TrimSpace(inner), 64)
		if err != nil {
			return "无法解析: " + expr
		}
		return strconv.FormatFloat(math.Sqrt(val), 'f', -1, 64)
	}

	// 处理二元运算
	for _, op := range []string{" + ", " - ", " * ", " / "} {
		if idx := strings.Index(expr, op); idx >= 0 {
			left, err1 := strconv.ParseFloat(strings.TrimSpace(expr[:idx]), 64)
			right, err2 := strconv.ParseFloat(strings.TrimSpace(expr[idx+len(op):]), 64)
			if err1 != nil || err2 != nil {
				continue
			}
			var result float64
			switch op {
			case " + ":
				result = left + right
			case " - ":
				result = left - right
			case " * ":
				result = left * right
			case " / ":
				if right == 0 {
					return "除数不能为零"
				}
				result = left / right
			}
			return strconv.FormatFloat(result, 'f', -1, 64)
		}
	}

	return "无法计算: " + expr
}

// --- 当前时间工具 ---

type currentTimeTool struct{}

func (t *currentTimeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "current_time",
		Desc: "获取当前日期和时间",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"timezone": {
				Type:     schema.String,
				Desc:     "时区，如 'Asia/Shanghai'、'UTC'，默认为本地时区",
				Required: false,
			},
		}),
	}, nil
}

func (t *currentTimeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Timezone string `json:"timezone"`
	}
	_ = json.Unmarshal([]byte(argumentsInJSON), &args)

	loc := time.Local
	if args.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(args.Timezone)
		if err != nil {
			return "", fmt.Errorf("无效时区 %q: %w", args.Timezone, err)
		}
	}

	now := time.Now().In(loc)
	return fmt.Sprintf("当前时间: %s (%s)", now.Format("2006-01-02 15:04:05"), loc.String()), nil
}

// =============================================================================
// 辅助函数
// =============================================================================

// getUserQuestion 从命令行参数获取问题，没传则用默认值
func getUserQuestion(defaultQ string) string {
	if len(os.Args) > 2 {
		return strings.Join(os.Args[2:], " ")
	}
	return defaultQ
}

func mustCreateModel(ctx context.Context) model.ToolCallingChatModel {
	modelName := "qwen2.5:3b"
	if env := os.Getenv("OLLAMA_MODEL"); env != "" {
		modelName = env
	}

	serverURL := "http://localhost:11434"
	if env := os.Getenv("OLLAMA_HOST"); env != "" {
		serverURL = env
	}

	chatModel, err := ollama.NewChatModel(ctx, &ollama.ChatModelConfig{
		BaseURL: serverURL,
		Model:   modelName,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建模型失败: %v\n", err)
		fmt.Fprintf(os.Stderr, "请确保 Ollama 已启动且模型 %s 已拉取\n", modelName)
		os.Exit(1)
	}

	return chatModel
}
