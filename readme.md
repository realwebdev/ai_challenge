# Go & Ollama Code Reviewer: Three Program Variants

This repository contains three separate Go programs designed to interface with a local Ollama server (`qwen2.5-coder:1.5b`) via its OpenAI-compatible endpoint. 

Rather than a single pipeline, these files represent three distinct architectural approaches (an HTTP server endpoint vs. two terminal CLI tools) showing different levels of completeness, streaming techniques, and telemetry gathering.

---

## 📁 Project Architecture At-a-Glance

| Program Component | Runtime Shape | Core Behavior | Primary Benefit |
| :--- | :--- | :--- | :--- |
| **`3-sse-review-endpoint/main.go`** | 🌐 HTTP Server | Live token streaming to a web client with strict lifecycle aborts. | Production-ready SSE delivery & GPU preservation. |
| **`2-stream/main.go`** | 💻 CLI Tool | Reads a local file and streams the review live to the terminal. | Immediate terminal UI feedback with ANSI colors. |
| **`1-chat-metrics/main.go`** | 💻 CLI Tool | Reads a local file, blocks for the full reply, and prints deep metrics. | Complete token telemetry tracking. |

---

## ⚙️ Detailed Variant Breakdown

### 🎯 1. HTTP Server: Live SSE Reviewer (`cmd/3-sse-review-endpoint/`)
Exposes a production-ready `POST /review` endpoint accepting a JSON payload containing `{ "code": "...", "language": "..." }`. It formats Ollama's response into a valid Server-Sent Events (SSE) stream.

* **Key Strengths:**
  * **Network Buffering Optimization**: Configures proper HTML5 SSE headers (`text/event-stream`) and invokes Go's `http.Flusher` to push data tokens down the wire instantly.
  * **Fragment Chunking**: Smartly buffers small text fragments, flushing to the client only when a boundary of ~60 characters or sentence punctuation is met to reduce net overhead.
  * **GPU Resource Preservation**: Binds the upstream network call using `http.NewRequestWithContext(r.Context(), ...)`. If a user closes the browser or hits `Ctrl+C`, the handler aborts instantly, canceling the Ollama job and reclaiming VRAM.
  * **Inline Metrics Tracking**: Sniffs out structural token metrics chunks from the stream and transmits them as an inline `[METRICS] ...` message.
* **Limitations:**
  * **Single-Turn Restrictive**: Always constructs a fresh system/user pair from the request. It does not ingest or retain chat history.
  * **Metric Volatility**: Telemetry messages rely heavily on Ollama choosing to append a `usage` block into its stream chunks.

### ⚡ 2. Terminal CLI: Live Token Streamer (`cmd/2-stream/`)
A CLI utility invoked via `go run main.go [filepath]`. It reads a local Go file from disk, dispatches it to Ollama with streaming enabled, and pushes the text straight into stdout.

* **Key Strengths:**
  * **True Terminal Streaming**: Decodes the SSE packet lines sequentially, printing raw string deltas as they hit the network buffer.
  * **Visual Cleanliness**: Implements ANSI color-coded layouts to structure terminal segment blocks.
  * **Latency Tracking**: Measures and outputs basic wall-clock duration processing metrics.
* **Limitations:**
  * **No Token Extraction**: Lacks a `Usage` field structure entirely within its JSON parser; prompt and completion token totals are discarded.
  * **No Cancellation Handling**: Lacks Context wiring. Interrupting the tool leaves the upstream generation hanging until completion.

### 📊 3. Terminal CLI: Telemetry & Metrics Tracker (`cmd/1-chat-metrics/`)
A CLI utility that reads a local file from your machine but rejects live text streaming. It blocks execution completely until the response concludes, prioritizing exact resource metrics over execution speed.

* **Key Strengths:**
  * **Deep Telemetry Mapping**: Explicitly maps out an OpenAI-compliant `UsageMetrics` data structure, tracking `prompt_tokens`, `completion_tokens`, and `total_tokens`.
  * **Structured Logs**: Outputs a dedicated, distinct `TELEMETRY & METRICS` terminal display showing total processing latency side-by-side with exact hardware billing tokens.
* **Limitations:**
  * **Blocking UX**: The terminal remains frozen with no visual indicator while waiting for the model to generate its entire review text.

---

## 🚀 Setting Up Your System

### 1. Enable Concurrency on Ollama
Ollama processes requests sequentially by default. To prevent blocking issues when running multiple instances or testing concurrent client streaming requests, configure your hardware worker limits before initializing the model service:

```bash
# macOS / Linux terminal configuration
export OLLAMA_NUM_PARALLEL=4
ollama serve

# Windows PowerShell configuration
\$env:OLLAMA_NUM_PARALLEL="4"
ollama serve
```

### 2. Execute a Variant Target
To spin up the web-service endpoint:
```bash
go run cmd/3-sse-review-endpoint/main.go
```

To invoke a terminal file analysis using one of the CLI variants:
```bash
go run cmd/2-stream/main.go ./testfile.go
```
