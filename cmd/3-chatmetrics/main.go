package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

const (
	ollamaURL = "http://localhost:11434/v1/chat/completions"
	modelName = "qwen2.5-coder:7b"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type UsageMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OllamaStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *UsageMetrics `json:"usage,omitempty"`
}

type ReviewRequest struct {
	Code     string `json:"code"`
	Language string `json:"language"`
}

func reviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// The regular http.ResponseWriter doesn't always know how to flush.
	// normally will wait for the buffer to get fil and suddenly response
	// shows up.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	systemPrompt := `You are a senior Go engineer. Review the provided code
	for readability,structure, and maintainability. Output 3 concise 
	improvements and 1 positive note.`

	userPrompt := fmt.Sprintf("Review this %s code:\n```go\n%s\n```", req.Language, req.Code)

	ollamaReq := OllamaRequest{
		Model: modelName,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Stream: true,
	}

	payload, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "failed to encode request", http.StatusInternalServerError)
		return
	}

	httpReq, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPost,
		ollamaURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		http.Error(w, "ollama returned an error", http.StatusBadGateway)
		return
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		http.Error(w, "ollama returned an error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "ollama returned an error", http.StatusBadGateway)
		return
	}

	scanner := bufio.NewScanner(resp.Body)

	var buffer strings.Builder

	for scanner.Scan() {
		// 1. Check if client disconnected before handling this chunk
		select {
		case <-r.Context().Done():
			log.Println(" Alert: Client disconnected")
			return // Exit the handler immediately
		default:
			// Client still connected, proceed normally
		}
		// TODO: Convert the unstructured stream into

		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			break
		}

		var chunk OllamaStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			metricsMsg := fmt.Sprintf("[METRICS] Prompt: %d | Completion: %d | Total: %d",
				chunk.Usage.PromptTokens,
				chunk.Usage.CompletionTokens,
				chunk.Usage.TotalTokens,
			)
			fmt.Fprintf(w, "data: %s\n\n", metricsMsg)
			// Forces the server to bypass that buffer.
			// It manually pushes whatever token or word
			// is currently sitting in memory down the network wire instantly
			flusher.Flush()
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content

			if content != "" {
				buffer.WriteString(content)

				text := buffer.String()

				// Flush when we have a reasonably sized chunk
				// or reach a natural sentence boundary.
				if len(text) >= 60 ||
					strings.ContainsAny(text, ".!?\n") {

					fmt.Fprintf(w, "data: %s\n\n", text)
					flusher.Flush()

					buffer.Reset()
				}
			}
		}
	}

	if buffer.Len() > 0 {
		fmt.Fprintf(w, "data: %s\n\n", buffer.String())
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(w, "data: stream error: %s\n\n", err)
		flusher.Flush()
	}
}

func main() {
	http.HandleFunc("/review", reviewHandler)
	fmt.Println("Code reviewer listening on :8084")
	if err := http.ListenAndServe(":8084", nil); err != nil {
		panic(err)
	}
}
