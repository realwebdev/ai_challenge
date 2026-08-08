# Go Agents on MCP SSE
mcp uses JSON-RPC 2.0. The SSE transport requires an endpoint to establish the event stream(/sse) and a corresponding endpoint to send (/messages/) to receive client commands.

I used native net/http to manage persistent client connection via Go
channels to stream JSON-RPC 2.0 notifications and tool declaration back to the mcp host(claude desktop or custom client).

## The Model Engine
### Ollama intetegration.
instead of relying on closed APIs , I pointed MCP server logic to a local ollama instance(qwen 2.5-coder).

## Context and state management


