# MLX Backend Architecture

This document describes the technical architecture of ollmlx's MLX integration.

## Overview

ollmlx extends Ollama's inference layer to support Apple's MLX framework while maintaining 100% API compatibility. The integration uses a hybrid architecture where the Go server layer proxies requests to either the traditional llama.cpp backend (for GGUF models) or a Python MLX backend (for MLX models).

**Runtime parity highlights (current implementation):**
- `server/routes_mlx.go` starts the MLX runner on a random local port, waits for `/health`, and calls `/load` before serving completions.
- Streaming completions are proxied line-for-line into Ollama's `GenerateResponse` NDJSON format (metrics populated in the `Metrics` block; logprobs passthrough-ready).
- Non-streaming completions aggregate the content and return the final `GenerateResponse` just like the GGUF path.
- Pull progress now uses a stable digest (SHA-256 of model name) so CLI progress bars behave like standard `ollama pull`.
- Experimental `/finetune` endpoint calls into `mlx_lm` fine-tune if available; returns 501 otherwise.
- MLX backend attempts to default to Metal GPU at startup for acceleration.
- `ollmlx run --verbose` surfaces Apple Silicon / MLX tuning tips (Metal, 4-bit MLX models, cache location).
- See [Apple Silicon Optimization Guide](./apple_silicon_optimization.md) for practical steps to extract more performance from MLX on macOS.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                      Client Layer                                │
│  (CLI, HTTP API, IDE Extensions, Python/JS Libraries)           │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    HTTP Server (Go)                              │
│  - Gin router handling /api/* endpoints                         │
│  - Request validation and routing                               │
│  - Streaming response management                                │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│              Model Format Detection (Go)                         │
│  - llm/detection.go: DetectModelFormat()                        │
│  - Checks file extension, directory structure, model name       │
│  - Returns: ModelFormatGGUF or ModelFormatMLX                   │
└────────────────────────────┬────────────────────────────────────┘
                             │
                    ┌────────┴────────┐
                    │                 │
                    ▼                 ▼
         ┌──────────────────┐  ┌──────────────────┐
         │  GGUF Path       │  │   MLX Path       │
         └──────────────────┘  └──────────────────┘
                    │                 │
                    ▼                 ▼
    ┌─────────────────────────────────────────────┐
    │      llama.cpp Runner        │    MLX Runner│
    │   (C++ via CGO)              │    (Go Proxy)│
    │  ├─ Load GGUF file           │  ├─ Spawn    │
    │  ├─ Token generation         │  │  Python   │
    │  └─ KV cache                 │  └─ HTTP     │
    └──────────────────────────────────┬──────────┘
                                       │
                                       ▼
                        ┌──────────────────────────┐
                        │  MLX Backend (Python)    │
                        │  - FastAPI server        │
                        │  - MLX model loading     │
                        │  - Token generation      │
                        │  - Streaming responses   │
                        └──────────────────────────┘
```

## Component Breakdown

### 1. Model Format Detection (`llm/detection.go`)

**Purpose**: Automatically determine whether a model is GGUF or MLX format.

**Detection Logic**:
```go
func DetectModelFormat(modelPath string) ModelFormat {
    // Check file extension
    if ext == ".gguf" → return GGUF

    // Check for MLX directory structure
    if isDirectory && hasConfigJSON && (hasSafetensors || hasWeights.npz) → return MLX

    // Check for HuggingFace reference
    if hasPrefix("mlx-community/") → return MLX
    if contains("-mlx") → return MLX

    // Default to GGUF for backward compatibility
    return GGUF
}
```

**Files Checked for MLX**:
- `config.json` (required)
- `model.safetensors` OR `weights.npz` (required)
- `tokenizer.json`, `tokenizer_config.json` (optional)

### 2. MLX Runner Bridge (`runner/mlxrunner/runner.go`)

**Purpose**: Go HTTP server that manages the Python MLX backend subprocess.

**Responsibilities**:
- Start Python MLX backend on a dynamic port
- Proxy HTTP requests from Ollama server to MLX backend
- Stream responses back to clients
- Handle subprocess lifecycle (start, health checks, shutdown)

**Key Methods**:
```go
func (s *Server) startMLXBackend(ctx context.Context) error
    - Finds python3 executable
    - Allocates random port for MLX backend
    - Starts mlx_backend/server.py as subprocess
    - Waits for health check (30 retries @ 500ms)

func (s *Server) completion(w http.ResponseWriter, r *http.Request)
    - Proxies POST /completion to MLX backend
    - Streams JSON-Lines response back to client
    - Maintains Ollama's CompletionResponse format

func (s *Server) load(w http.ResponseWriter, r *http.Request)
    - Sends model path to MLX backend
    - MLX backend loads model from disk or downloads from HF
```

**Port Allocation**:
- Go runner: Dynamic port (allocated by Ollama's StartRunner)
- Python MLX backend: Dynamic port (allocated by mlxrunner)
- Communication: Go ← HTTP → Python (localhost only)

### 3. MLX Backend (`mlx_backend/server.py`)

**Purpose**: Python FastAPI server that handles MLX model inference.

**Architecture**:
```python
FastAPI App
├── /health - Health check endpoint
├── /info - GPU/device information
├── /load - Load model from path or HuggingFace
└── /completion - Generate tokens with streaming

Model Manager
├── model cache (singleton)
├── tokenizer cache
└── HuggingFace integration
```

**Request Flow**:
1. **Load Request**:
   ```json
   POST /load
   {"model_path": "mlx-community/Llama-3.2-1B-Instruct-4bit"}
   ```
   - Checks local cache (~/.ollama/models/mlx/)
   - If not found, downloads from HuggingFace
   - Loads model and tokenizer into memory
   - Uses MLX's quantization (4-bit, 8-bit, etc.)

2. **Completion Request**:
   ```json
   POST /completion
   {
     "prompt": "Why is the sky blue?",
     "options": {"temperature": 0.7, "num_predict": 128}
   }
   ```
   - Tokenizes prompt
   - Generates tokens one at a time using MLX
   - Streams CompletionResponse JSON for each token
   - Final response includes timing metrics

**MLX Optimizations**:
- **Unified Memory**: MLX uses Apple's unified memory architecture
- **Metal Performance Shaders**: GPU acceleration via Metal
- **Quantization**: Native support for 4-bit, 8-bit quantized models
- **Lazy Evaluation**: MLX's lazy computation graph minimizes memory copies

### 4. Server Integration (`llm/server.go`)

**NewServer Function**:
```go
func NewServer(systemInfo, gpus, modelPath, ...) (LlamaServer, error) {
    if IsMLXModel(modelPath) {
        return NewMLXServer(...)  // MLX path
    }
    return NewLlamaServer(...)    // GGUF path
}
```

**NewMLXServer**:
- Calls `StartRunnerWithEngine(mlxEngine=true)`
- Starts mlxrunner subprocess
- Returns LlamaServer interface (compatible with existing code)
- No GPU libraries needed (MLX handles Metal directly)

### 5. HTTP API Routes (`server/routes.go`)

**Pull Handler**:
```go
func PullHandler(c *gin.Context) {
    if IsMLXModelReference(modelName) {
        PullMLXModel(modelName)  // HuggingFace download
    } else {
        PullModel(modelName)     // Ollama registry
    }
}
```

**List Handler**:
```go
func ListHandler(c *gin.Context) {
    ggufModels := Manifests()      // Standard Ollama models
    mlxModels := ListMLXModels()   // MLX models from cache
    return append(ggufModels, mlxModels)
}
```

**Delete/Show Handlers**:
- Check `IsMLXModelReference()`
- Route to MLX manager or standard Ollama path

## Data Flow

### Generate Request (MLX Model)

```
1. Client → POST /api/generate {"model": "mlx-community/Llama-3.2-1B"}
   ↓
2. server/routes.go: GenerateHandler()
   ↓
3. server/sched.go: GetRunner()
   ↓
4. llm/server.go: DetectModelFormat() → MLX
   ↓
5. llm/server.go: NewMLXServer()
   ↓
6. runner/mlxrunner/runner.go: Start subprocess
   ├─ Spawn: python3 mlx_backend/server.py --port 9090
   └─ Wait for health check
   ↓
7. llm/server.go: Load() → POST http://localhost:9090/load
   ↓
8. mlx_backend/server.py: Load model from HF or cache
   ↓
9. llm/server.go: Completion() → POST http://localhost:9090/completion
   ↓
10. mlx_backend/server.py: MLX token generation (streaming)
    ├─ Tokenize prompt
    ├─ For each token: mlx.generate()
    └─ Stream: {"content": "...", "done": false}
   ↓
11. runner/mlxrunner: Proxy response back to Ollama server
   ↓
12. server/routes.go: StreamResponse to client (SSE format)
   ↓
13. Client receives tokens
```

### Pull Request (MLX Model)

```
1. Client → POST /api/pull {"name": "mlx-community/Llama-3.2-1B"}
   ↓
2. server/routes.go: PullHandler()
   ↓
3. server/routes_mlx.go: IsMLXModelReference() → true
   ↓
4. llm/mlx_models.go: DownloadMLXModel()
   ├─ Create ~/.ollama/models/mlx/mlx-community_Llama-3.2-1B/
   ├─ Download config.json
   ├─ Download tokenizer files
   ├─ Download model weights (safetensors/npz)
   └─ Progress callbacks → Stream to client
   ↓
5. Model cached locally and ready to use
```

## Performance Characteristics

### MLX Backend
- **Latency**: ~50-100ms startup (model loading)
- **Throughput**: 20-100 tokens/sec (model dependent)
- **Memory**: Unified memory (GPU/CPU share pool)
- **Quantization**: 4-bit typical, 8-bit for larger models

### IPC Overhead
- **HTTP Proxy**: ~1-5ms per request
- **Streaming**: Minimal buffering (JSON-Lines)
- **Subprocess**: Started once, reused for multiple requests

### Comparison to GGUF
On Apple Silicon (M1/M2/M3):
- **MLX**: 20-40% faster token generation
- **GGUF**: Compatible with all platforms
- **Trade-off**: MLX requires Python runtime

## Error Handling

### Subprocess Failures
```go
// mlxrunner monitors subprocess health
if mlxCmd.Process.Exited() {
    return ErrMLXBackendCrashed
}

// Automatic restart on health check failure
for i := 0; i < 30; i++ {
    if healthCheck() == OK { break }
    time.Sleep(500ms)
}
```

### Model Loading Errors
- **Not Found**: Return 404, suggest HuggingFace search
- **Download Failed**: Retry with exponential backoff
- **OOM**: Clear error message with model size requirements

### API Compatibility Errors
- All errors match Ollama's error format
- HTTP status codes identical to Ollama
- Error messages maintain user-friendly language

## Security Considerations

### Subprocess Isolation
- MLX backend runs as child process (same user)
- Communicates only via localhost
- No external network access required

### Model Downloads
- HuggingFace HTTPS only
- Checksum validation (when available)
- User-controlled model directory

### API Surface
- No new endpoints (100% Ollama API)
- Standard authentication (if configured)
- Rate limiting (Ollama's built-in)

## Testing Strategy

### Unit Tests
- `llm/detection_test.go`: Format detection
- `runner/mlxrunner/runner_test.go`: HTTP proxy logic

### Integration Tests
- `test/mlx_integration_test.go`: End-to-end MLX flow
- Requires Python environment and MLX installation
- Uses small test models (<1GB)

### Compatibility Tests
- Compare MLX vs GGUF outputs (same prompt)
- Verify JSON schema compliance
- Test with Ollama Python/JS clients

## Future Enhancements

### Planned
1. **gRPC Communication**: Replace HTTP with gRPC for lower latency
2. **Model Caching**: Keep popular models in memory
3. **Batch Inference**: Support multiple concurrent requests
4. **Quantization Options**: User-selectable quantization levels

### Under Consideration
1. **Direct MLX Bindings**: Go bindings to MLX (if available)
2. **Multi-GPU Support**: Distribute model across multiple GPUs
3. **Custom Quantization**: Fine-tune quantization per layer

## Debugging

### Enable Verbose Logging
```bash
export OLLAMA_DEBUG=1
./ollama serve
```

### Check MLX Backend
```bash
# Test MLX backend directly
cd mlx_backend
python server.py --port 9090

# In another terminal
curl http://localhost:9090/health
curl http://localhost:9090/info
```

### Monitor Subprocess
```bash
# Check if Python process is running
ps aux | grep "mlx_backend/server.py"

# View logs
tail -f ~/.ollama/logs/server.log
```

## References

- [Ollama Architecture](https://github.com/ollama/ollama/blob/main/docs/development.md)
- [MLX Documentation](https://ml-explore.github.io/mlx/build/html/index.html)
- [FastAPI Documentation](https://fastapi.tiangolo.com/)
- [Metal Performance Shaders](https://developer.apple.com/documentation/metalperformanceshaders)
