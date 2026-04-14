# Eino Demo

基于 [Eino](https://github.com/cloudwego/eino) 框架的 AI 应用开发 Demo，使用 Ollama 本地模型运行。

Eino 是字节跳动 CloudWeGo 开源的 Go 语言 AI 应用开发框架，提供组件抽象、图编排、流式处理、AOP 切面等能力。

## 包含的 Demo

| Demo | 场景 | 覆盖的 Eino 概念 |
|------|------|-----------------|
| **chat** | 基础同步对话 | ChatModel, Message, Generate |
| **stream** | 流式输出（打字机效果） | Stream, StreamReader |
| **agent** | 工具调用 Agent（ReAct 循环） | ReAct Agent, InvokableTool, ToolsNodeConfig |
| **rag** | 检索增强生成（Graph 编排） | Graph, State, Lambda, ChatTemplate, Retriever, Embedding |

## 前置依赖

- Go 1.24+
- [Ollama](https://ollama.com/) 已安装并运行

```bash
# 安装 Ollama（macOS）
brew install ollama
brew services start ollama

# 拉取模型
ollama pull qwen2.5:3b           # 对话模型
ollama pull nomic-embed-text     # Embedding 模型（RAG demo 需要）
```

## 运行

### 基础 Demo（chat / stream / agent）

三个命令都支持自定义问题，不传则使用默认问题：

```bash
# 基础对话（同步生成）
go run . chat
go run . chat 什么是微服务架构？

# 流式输出（打字机效果）
go run . stream
go run . stream 用三句话介绍Docker

# 工具调用 Agent（自带计算器 + 时间查询工具）
go run . agent
go run . agent 现在几点？帮我算一下 100 除以 7
```

### RAG Demo（Graph 编排 + 向量检索）

内置了一组 Eino 框架相关知识文档，支持自定义问题：

```bash
# 使用默认问题（会问 4 个关于 Eino 的问题）
go run ./cmd/rag/

# 自定义问题
go run ./cmd/rag/ Eino 支持哪些编排模式？
```

RAG 流水线架构：

```
START → retriever(向量检索) → formatter(格式化) → template(Prompt渲染) → llm(生成回答) → END
  │          ↑↓                                        ↑
  │    State: 存 Query                           State: 读 Query
  └──────────── WithGenLocalState 跨节点传递 ───────────┘
```

## 项目结构

```
eino-demo/
├── main.go              # 基础 Demo（chat / stream / agent）
├── cmd/rag/main.go      # RAG Demo（Graph 编排 + 内存向量检索）
├── docs/                # 研究报告
├── go.mod
└── go.sum
```

## 环境变量

| 变量 | 默认值 | 说明 | 适用 |
|------|--------|------|------|
| `OLLAMA_MODEL` | `qwen2.5:3b` | 对话模型名 | 全部 |
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama 服务地址 | 全部 |
| `EMBEDDING_MODEL` | `nomic-embed-text` | Embedding 模型名 | RAG |

## 覆盖的 Eino 核心概念

- **schema.Message** — 消息构造（System / User / Assistant / Tool）
- **ChatModel** — `Generate()`（同步）/ `Stream()`（流式）
- **ToolCallingChatModel** — 支持工具调用的模型接口
- **InvokableTool** — 自定义工具实现（`Info()` + `InvokableRun()`）
- **ReAct Agent** — 自动 LLM → Tool → LLM 循环
- **Graph 编排** — `NewGraph` + `AddEdge` + `Compile` → `Runnable`
- **State Graph** — `WithGenLocalState` + `StatePreHandler` / `PostHandler`
- **Lambda** — `InvokableLambda` 类型桥接
- **ChatTemplate** — `prompt.FromMessages` + FString 模板
- **Runnable** — 编译后支持 `Invoke`（同步）和 `Stream`（流式）
