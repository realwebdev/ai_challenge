package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpListenAndServe starts the MCP server for all the tooling we support.
func mcpListenAndServe(host string) {
	fmt.Printf("\nServer: MCP server serving at %s\n", host)

	fileOperations := mcp.NewServer(&mcp.Implementation{Name: "file_operations", Version: "v1.0.0"}, nil)
	routePath := RegisterSmartCodeReviewerTool(fileOperations)

	f := func(request *http.Request) *mcp.Server {
		url := request.URL.Path
		if url == routePath {
			return fileOperations
		}
		return mcp.NewServer(&mcp.Implementation{Name: "unknown_tool", Version: "v1.0.0"}, nil)
	}

	handler := mcp.NewSSEHandler(f, &mcp.SSEOptions{})
	if err := http.ListenAndServe(host, handler); err != nil {
		fmt.Printf("HTTP server error: %v\n", err)
	}
}

// =============================================================================

// RegisterSmartCodeReviewerTool registers the tool with the given MCP server.
func RegisterSmartCodeReviewerTool(mcpServer *mcp.Server) string {
	const toolName = "tool_smart_code_reviewer"
	const toolDescription = "Reviews and refactors code for readability, structure, and maintainability."

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        toolName,
		Description: toolDescription,
	}, SmartCodeReviewerHandler)

	return "/" + toolName
}

// SmartCodeReviewerToolParams represents the parameters for the tool call.
type SmartCodeReviewerToolParams struct {
	Path            *string `json:"path,omitempty" jsonschema:"Relative path and name of the Golang file"`
	LineNumber      int     `json:"line_number" jsonschema:"The line number for the code review modification"`
	ActionType      *string `json:"action_type,omitempty" jsonschema:"Type of review action: readability, structure, or maintainability"`
	SuggestedChange *string `json:"suggested_change,omitempty" jsonschema:"The proposed refactored code line or block"`
}

// SmartCodeReviewerHandler applies code updates based on AI code review feedback.
func SmartCodeReviewerHandler(ctx context.Context, req *mcp.CallToolRequest, params SmartCodeReviewerToolParams) (*mcp.CallToolResult, any, error) {
	filePath := "."
	if params.Path != nil && *params.Path != "" {
		filePath = *params.Path
	}

	lineNumber := params.LineNumber
	actionType := "readability"
	if params.ActionType != nil {
		actionType = strings.ToLower(strings.TrimSpace(*params.ActionType))
	}

	suggestedChange := ""
	if params.SuggestedChange != nil {
		suggestedChange = strings.TrimSpace(*params.SuggestedChange)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	fset := token.NewFileSet()
	lines := strings.Split(string(content), "\n")

	if lineNumber < 1 || lineNumber > len(lines) {
		return nil, nil, fmt.Errorf("line number %d is out of range (1-%d)", lineNumber, len(lines))
	}

	switch actionType {
	case "readability":
		// Insert line for better readability spacing/comments
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:lineNumber-1]...)
		newLines = append(newLines, suggestedChange)
		newLines = append(newLines, lines[lineNumber-1:]...)
		lines = newLines

	case "structure":
		// Restructure/replace specific line(s)
		lines[lineNumber-1] = suggestedChange

	case "maintainability":
		// Remove redundant line to improve maintainability
		if len(lines) == 1 {
			lines = []string{""}
		} else {
			lines = append(lines[:lineNumber-1], lines[lineNumber:]...)
		}

	default:
		return nil, nil, fmt.Errorf("unsupported review action type: %s", actionType)
	}

	modifiedContent := strings.Join(lines, "\n")

	// Verify syntax integrity using go/parser
	_, err = parser.ParseFile(fset, filePath, modifiedContent, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("syntax validation failed after modification: %w", err)
	}

	// Format code using go/format
	formattedContent, err := format.Source([]byte(modifiedContent))
	if err != nil {
		formattedContent = []byte(modifiedContent)
	}

	err = os.WriteFile(filePath, formattedContent, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write file: %w", err)
	}

	info := struct {
		Status     string `json:"status"`
		Action     string `json:"action"`
		LineNumber int    `json:"line_number"`
	}{
		Status:     "success",
		Action:     actionType,
		LineNumber: lineNumber,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: string(data),
		}},
	}, nil, nil
}
