package main

import (
	"context"
	"encoding/json"
	"fmt"
	"realwebdev/ai_challenge/foundation/client"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClient is a client for the MCP server.
type mcpClient struct {
	client *mcp.Client
}

// newMCPClient constructs a new MCP client.
func newMCPClient() *mcpClient {
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)

	return &mcpClient{
		client: client,
	}
}

// Call executes an MCP tool call using the provided transport and parameters.
func (cln *mcpClient) Call(ctx context.Context, transport *mcp.SSEClientTransport, params *mcp.CallToolParams) ([]mcp.Content, error) {
	fmt.Print("\u001b[92mtool: connecting to MCP Server\u001b[0m\n")

	session, err := cln.client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MCP server: %w", err)
	}
	defer session.Close()

	fmt.Printf("\u001b[92mtool: calling tool: %s(%v)\u001b[0m\n", params.Name, params.Arguments)

	res, err := session.CallTool(ctx, params)
	if err != nil {
		fmt.Printf("\u001b[91mtool: error calling tool: %v\u001b[0m\n", err)
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}

	if res.IsError {
		fmt.Printf("\u001b[91mtool: result is error\u001b[0m\n")
		return nil, fmt.Errorf("tool call failed: %s", res.Content)
	}

	return res.Content, nil
}

// =============================================================================

// toolSuccessResponse returns a successful structured tool response.
func toolSuccessResponse(toolID string, keyValues ...any) client.D {
	data := make(map[string]any)
	for i := 0; i < len(keyValues); i = i + 2 {
		data[keyValues[i].(string)] = keyValues[i+1]
	}

	return toolResponse(toolID, data, "SUCCESS")
}

// toolErrorResponse returns a failed structured tool response.
func toolErrorResponse(toolID string, err error) client.D {
	data := map[string]any{"error": err.Error()}

	return toolResponse(toolID, data, "FAILED")
}

// toolResponse creates a structured tool response.
func toolResponse(toolID string, data map[string]any, status string) client.D {
	info := struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}{
		Status: status,
		Data:   data,
	}

	content, err := json.Marshal(info)
	if err != nil {
		return client.D{
			"role":         "tool",
			"tool_call_id": toolID,
			"content":      `{"status": "FAILED", "data": "error marshaling tool response"}`,
		}
	}

	return client.D{
		"role":         "tool",
		"tool_call_id": toolID,
		"content":      string(content),
	}
}

// =============================================================================
// ReadFile Tool

// ReadFile represents a tool that can be used to read the contents of a file.
type ReadFile struct {
	name      string
	mcpClient *mcpClient
	transport *mcp.SSEClientTransport
}

// RegisterSmartCodeReviewer creates a new instance of the ReadFile tool and loads it
// into the provided tools map.
func RegisterSmartCodeReviewer(mcpClient *mcpClient, tools map[string]Tool) client.D {
	toolName := "tool_smart_code_reviewer"

	addr := fmt.Sprintf("http://%s/%s", mcpHost, toolName)
	transport := mcp.SSEClientTransport{
		Endpoint: addr,
	}

	rf := ReadFile{
		name:      toolName,
		mcpClient: mcpClient,
		transport: &transport,
	}
	tools[rf.name] = &rf

	return rf.toolDocument()
}

// ToolDocument defines the metadata for the tool that is provied to the model.
func (rf *ReadFile) toolDocument() client.D {
	return client.D{
		"type": "function",
		"function": client.D{
			"name":        rf.name,
			"description": "Read the contents of a given file path or search for files containing a pattern. When searching file contents, returns line numbers where the pattern is found.",
			"parameters": client.D{
				"type": "object",
				"properties": client.D{
					"path": client.D{
						"type":        "string",
						"description": "The relative path of a file in the working directory. If pattern is provided, this can be a directory path to search in.",
					},
				},
				"required": []string{"path"},
			},
		},
	}
}

// Call is the function that is called by the agent to read the contents of a
// file when the model requests the tool with the specified parameters.
func (rf *ReadFile) Call(ctx context.Context, tool client.ToolCall) (resp client.D) {
	defer func() {
		if r := recover(); r != nil {
			resp = toolErrorResponse(tool.ID, fmt.Errorf("%s", r))
		}
	}()

	params := mcp.CallToolParams{
		Name:      rf.name,
		Arguments: tool.Function.Arguments,
	}

	results, err := rf.mcpClient.Call(ctx, rf.transport, &params)
	if err != nil {
		return toolErrorResponse(tool.ID, fmt.Errorf("failed to call tool: %w", err))
	}

	data := results[0].(*mcp.TextContent).Text

	var info struct {
		Contents string `json:"contents"`
	}

	if err := json.Unmarshal([]byte(data), &info); err != nil {
		return toolErrorResponse(tool.ID, fmt.Errorf("failed to unmarshal response: %w", err))
	}

	return toolSuccessResponse(tool.ID, "file_contents", info.Contents)
}

