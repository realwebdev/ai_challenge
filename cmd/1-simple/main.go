package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Configuration matching your local environment
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

type OllamaResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type ReviewRequest struct {
	Code     string `json:"code"`
	Language string `json:"language"`
}

func main() {
	filePath := "main.go" // Default file to review
	if len(os.Args) > 1 {
		filePath = os.Args[1]
	}

	fmt.Printf("\033[36m=== STARTING CODE REVIEWER AI ===\033[0m\n")
	fmt.Printf("[*] Target File: %s\n", filePath)
	fmt.Printf("[*] Model: %s\n\n", modelName)

	// 1. Read file contents
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("\033[31m[ERROR] Failed to read file: %v\033[0m\n", err)
		return
	}

	codeContent := string(contentBytes)

	// 2. Construct Prompt
	systemPrompt := "You are a senior Go engineer. Review the provided code for readability, structure, and maintainability. Output 3 concise improvements and 1 positive note."
	userPrompt := fmt.Sprintf("Review this code:\n```go\n%s\n```", codeContent)

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	reqPayload := OllamaRequest{
		Model:    modelName,
		Messages: messages,
		Stream:   true,
	}

	jsonPayload, _ := json.Marshal(reqPayload)

	// 3. Telemetry: Track Request Start Time
	start := time.Now()
	fmt.Printf("\033[33m[TOOL CALL] Sending request to local Ollama instance...\033[0m\n")

	resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		fmt.Printf("\033[31m[ERROR] Connection failed: %v. Is 'ollama serve' running?\033[0m\n", err)
		return
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		fmt.Printf("\033[31m[ERROR] Failed to parse response: %v\033[0m\n", err)
		return
	}

	// 4. Output Transparent Telemetry & Results
	fmt.Printf("\n\033[32m=== TELEMETRY & METRICS ===\033[0m\n")
	fmt.Printf(" ├── Latency:          %v\n", duration)
	fmt.Printf(" ├── Prompt Tokens:    %d\n", ollamaResp.Usage.PromptTokens)
	fmt.Printf(" ├── Completion Tokens:%d\n", ollamaResp.Usage.CompletionTokens)
	fmt.Printf(" └── Total Token Burn: %d tokens\n", ollamaResp.Usage.TotalTokens)

	fmt.Printf("\n\033[35m=== AI CODE REVIEW OUTPUT ===\033[0m\n")
	if len(ollamaResp.Choices) > 0 {
		fmt.Println(ollamaResp.Choices[0].Message.Content)
	} else {
		fmt.Println("No response content generated.")
	}
}
