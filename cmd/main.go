// This example shows you how to tooling to a MCP service that is called by the tooling.
//
// # Running the example:


package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"realwebdev/ai_challenge/foundation/client"
	"strings"
	"sync"
	"time"
)

var (
    url     = "http://localhost:11434/v1/chat/completions" // default Ollama port
    model   = "qwen2.5-coder:1.5b"                         // downloaded model
    mcpHost = "localhost:8082"                           // local MCP SSE port
)
func init() {
	if v := os.Getenv("LLM_SERVER"); v != "" {
		url = v
	}

	if v := os.Getenv("LLM_MODEL"); v != "" {
		model = v
	}

	if v := os.Getenv("MCP_HOST"); v != "" {
		mcpHost = v
	}
}

// =============================================================================

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

	agent, err := NewAgent(getUserMessage)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	return agent.Run(context.TODO())
}

// =============================================================================

// Tool describes the features which all tools must implement.
type Tool interface {
	Call(ctx context.Context, toolCall client.ToolCall) client.D
}

// =============================================================================

// Agent represents the chat agent that can use tools to perform tasks.
type Agent struct {
	sseClient      *client.SSEClient[client.ChatSSE]
	mcpClient      *mcpClient
	getUserMessage func() (string, bool)
	tools          map[string]Tool
	toolDocuments  []client.D
	modelInfo      modelInfo
}

type modelInfo struct {
	id string
}

// NewAgent creates a new instance of Agent.
func NewAgent(getUserMessage func() (string, bool)) (*Agent, error) {

	// -------------------------------------------------------------------------
	// Build tool documents by registering each tool with its own tools map.

	toolsMap := make(map[string]Tool)
	mcpClient := newMCPClient()
	toolDocuments := []client.D{
		RegisterSmartCodeReviewer(mcpClient, toolsMap),
	}

	agent := Agent{
		sseClient:      client.NewSSE[client.ChatSSE](client.StdoutLogger),
		mcpClient:      mcpClient,
		getUserMessage: getUserMessage,
		tools:          toolsMap,
		toolDocuments:  toolDocuments,
		modelInfo: modelInfo{
			id: model,
		},
	}

	return &agent, nil
}

// systemPrompt defines how the agent should behave when assisting with coding tasks.
const systemPrompt = `
	You are a senior staff engineer performing a pre-human code review using your available tools.
Review the provided Go source code ONLY for: readability, structure, and maintainability — ignore functional correctness or runtime bugs.

### Operational Rules:
1. First, inspect the file using your tool (tool_smart_code_reviewer) if modifications or targeted reviews are required.
2. Always count lines of code starting from 1 at the top of the file.
3. When you receive tool execution results, check the "status" field. If it fails, inform the user immediately and do not retry the tool.
4. Once the review is complete, your final response must be STRICT JSON matching this exact schema and nothing else (no markdown blocks like "json, no conversational prose):

{
  "summary": "one sentence overview",
  "issues": [
    {
      "category": "readability|structure|maintainability",
      "severity": "low|medium|high",
      "line": 0,
      "finding": "...",
      "suggestion": "..."
    }
  ],
  "positive_note": "one genuine positive thing about the code"
}

Return at most 5 issues, ordered strictly by severity descending (high to low).
`

// Run starts the agent and runs the chat loop.
func (a *Agent) Run(ctx context.Context) error {
	time.Sleep(time.Second)

	conversation := []client.D{
		{"role": "system", "content": systemPrompt},
	}

	fmt.Printf("\nChat with %s (use 'ctrl-c' to quit)\n", a.modelInfo.id)

	needUserInput := true

	for {
		// ---------------------------------------------------------------------
		// If we need user input, prompt the user for their next question or
		// request. Otherwise, we are continuing a tool call.

		if needUserInput {
			if ok := a.promptUser(&conversation); !ok {
				return nil
			}
		}

		// ---------------------------------------------------------------------
		// Make a streaming call to the model. This returns the assistant's
		// text content and any tool calls requested by the model.

		content, toolCalls, usage, err := a.streamModelTurn(ctx, conversation)
		if err != nil {
			return err
		}

		a.printUsage(usage)

		// ---------------------------------------------------------------------
		// If the model requested tool calls, execute them and continue the
		// loop without asking the user for input.

		if len(toolCalls) > 0 {
			a.appendToolCalls(&conversation, toolCalls)

			results := a.callTools(ctx, toolCalls)
			if len(results) > 0 {
				conversation = append(conversation, results...)
			}

			needUserInput = false
			continue
		}

		// ---------------------------------------------------------------------
		// The model produced a text response. Add it to the conversation
		// and go back to asking the user for input.

		a.appendAssistant(&conversation, content)

		needUserInput = true
	}
}

// promptUser asks the user for input and appends it to the conversation.
func (a *Agent) promptUser(conversation *[]client.D) bool {
	fmt.Print("\u001b[94m\nYou\u001b[0m: ")

	userInput, ok := a.getUserMessage()
	if !ok {
		return false
	}

	*conversation = append(*conversation, client.D{
		"role":    "user",
		"content": userInput,
	})

	return true
}

// streamModelTurn sends the conversation to the model and streams back the
// response. It returns the assembled text content, any tool calls, and usage.
func (a *Agent) streamModelTurn(ctx context.Context, conversation []client.D) (string, []client.ToolCall, *client.Usage, error) {
	d := client.D{
		"model":          model,
		"messages":       conversation,
		"temperature":    0.0,
		"top_p":          0.1,
		"top_k":          1,
		"stream":         true,
		"tools":          a.toolDocuments,
		"tool_selection": "auto",
	}

	fmt.Printf("\u001b[93m\n%s\u001b[0m: 0.000", a.modelInfo.id)

	callCtx, cancelCall := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelCall()

	ch := make(chan client.ChatSSE, 100)

	if err := a.sseClient.Do(callCtx, http.MethodPost, url, d, ch); err != nil {
		return "", nil, nil, fmt.Errorf("error streaming: %w", err)
	}

	// Start the latency printer and ensure it stops.
	stopPrinter := a.startLatencyPrinter(ctx)
	defer stopPrinter()

	var chunks []string
	var lastResp client.ChatSSE
	reasonFirstChunk := true
	reasonThinking := false

	for resp := range ch {
		lastResp = resp

		if len(resp.Choices) == 0 {
			continue
		}

		// On the first real chunk, stop the latency printer.
		stopPrinter()

		switch resp.Choices[0].FinishReason {
		case "error":
			return "", nil, lastResp.Usage, fmt.Errorf("error from model: %s", resp.Choices[0].Delta.Content)

		case "stop":
			text := strings.TrimLeft(strings.Join(chunks, " "), "\n")
			return text, nil, lastResp.Usage, nil

		case "tool_calls":
			return "", resp.Choices[0].Delta.ToolCalls, lastResp.Usage, nil

		default:
			delta := resp.Choices[0].Delta

			switch {
			case delta.Reasoning != "":
				reasonThinking = true

				if reasonFirstChunk {
					reasonFirstChunk = false
					fmt.Print("\n")
				}

				fmt.Printf("\u001b[91m%s\u001b[0m", delta.Reasoning)

			case delta.Content != "":
				if reasonThinking {
					reasonThinking = false
					fmt.Print("\n\n")
				}

				fmt.Print(delta.Content)
				chunks = append(chunks, delta.Content)
			}
		}
	}

	// Stream ended without an explicit finish reason.
	text := strings.TrimLeft(strings.Join(chunks, " "), "\n")
	return text, nil, lastResp.Usage, nil
}

// startLatencyPrinter starts a goroutine that displays elapsed time while
// waiting for the model's first response chunk. The returned function stops
// the printer; it is safe to call multiple times.
func (a *Agent) startLatencyPrinter(ctx context.Context) (stop func()) {
	modelID := a.modelInfo.id
	start := time.Now()

	ticker := time.NewTicker(100 * time.Millisecond)
	done := make(chan struct{})
	exited := make(chan struct{})

	var once sync.Once
	stop = func() {
		once.Do(func() {
			close(done)
			<-exited
		})
	}

	go func() {
		defer ticker.Stop()
		defer close(exited)

		for {
			select {
			case <-ticker.C:
				m := time.Since(start).Milliseconds()
				fmt.Printf("\r\u001b[93m%s %d.%03d\u001b[0m:  ", modelID, m/1000, m%1000)

			case <-done:
				fmt.Print("\n")
				return

			case <-ctx.Done():
				fmt.Print("\n")
				return
			}
		}
	}()

	return stop
}

// appendToolCalls adds the assistant's tool call request to the conversation.
func (a *Agent) appendToolCalls(conversation *[]client.D, toolCalls []client.ToolCall) {
	fmt.Print("\n\n")

	var toolCallDocs []client.D
	for _, tc := range toolCalls {
		argsJSON, _ := json.Marshal(tc.Function.Arguments)
		toolCallDocs = append(toolCallDocs, client.D{
			"id":   tc.ID,
			"type": "function",
			"function": client.D{
				"name":      tc.Function.Name,
				"arguments": string(argsJSON),
			},
		})
	}

	*conversation = append(*conversation, client.D{
		"role":       "assistant",
		"tool_calls": toolCallDocs,
	})
}

// appendAssistant adds the assistant's text response to the conversation.
func (a *Agent) appendAssistant(conversation *[]client.D, content string) {
	if content == "" {
		return
	}

	fmt.Print("\n")
	*conversation = append(*conversation, client.D{"role": "assistant", "content": content})
}

// printUsage displays token usage information after each model call.
func (a *Agent) printUsage(usage *client.Usage) {
	if usage == nil {
		return
	}

	contextTokens := usage.PromptTokens + usage.CompletionTokens
	contextWindow := 32 * 1024 // TODO: Get this from model config when available
	percentage := (float64(contextTokens) / float64(contextWindow)) * 100
	of := float32(contextWindow) / float32(1024)

	fmt.Printf("\n\n\u001b[90mInput: %d  Reasoning: %d  Completion: %d  Output: %d  Window: %d (%.0f%% of %.0fK) TPS: %.2f\u001b[0m",
		usage.PromptTokens, usage.ReasoningTokens, usage.CompletionTokens, usage.OutputTokens, contextTokens, percentage, of, usage.TokensPerSecond)
}

// callTools looks up requested tools by name and executes them.
func (a *Agent) callTools(ctx context.Context, toolCalls []client.ToolCall) []client.D {
	resps := make([]client.D, 0, len(toolCalls))

	for _, toolCall := range toolCalls {
		tool, exists := a.tools[toolCall.Function.Name]
		if !exists {
			continue
		}

		fmt.Printf("\u001b[92m%s(%v)\u001b[0m:\n", toolCall.Function.Name, toolCall.Function.Arguments)
		resps = append(resps, tool.Call(ctx, toolCall))
	}

	return resps
}
