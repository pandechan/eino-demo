# Eino Demo

基于 [Eino](https://github.com/cloudwego/eino) 框架的 AI 应用开发 Demo，使用 Ollama 本地模型运行。

Eino 是字节跳动 CloudWeGo 开源的 Go 语言 AI 应用开发框架，提供组件抽象、图编排、流式处理、AOP 切面等能力。

## 包含的 Demo

| 命令 | 场景 | 覆盖的 Eino 概念 |
|------|------|-----------------|
| `chat` | 基础同步对话 | ChatModel, Message, Generate |
| `stream` | 流式输出（打字机效果） | Stream, StreamReader |
| `agent` | 工具调用 Agent（ReAct 循环） | ReAct Agent, InvokableTool, ToolsNodeConfig |

## 前置依赖

- Go 1.24+
- [Ollama](https://ollama.com/) 已安装并运行
- 拉取模型：`ollama pull qwen2.5:3b`

## 运行

```bash
# 基础对话
go run . chat

# 流式输出
go run . stream

# 工具调用 Agent（自带计算器 + 时间查询工具）
go run . agent
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `OLLAMA_MODEL` | `qwen2.5:3b` | Ollama 模型名 |
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama 服务地址 |
