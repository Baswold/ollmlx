# MLX Backend Server for ollmlx

## Overview

This is the MLX inference backend for ollmlx. It's a standalone Python service that provides MLX-based model inference while maintaining full API compatibility with Ollama's completion interface.

The backend runs as a subprocess and communicates with the main Ollama Go server via HTTP, receiving `CompletionRequest` JSON and streaming back `CompletionResponse` chunks in the exact same format as the llama.cpp runner.

## Architecture

```
Ollama Server (Go)
    │
    └─> HTTP POST to subprocess
        │
        ├─ localhost:DYNAMIC_PORT/completion
        ├─ localhost:DYNAMIC_PORT/load
        ├─ localhost:DYNAMIC_PORT/health
        └─ localhost:DYNAMIC_PORT/info
            │
            ▼
        MLX Backend Server (Python)
            │
            ├─ Load MLX models from Hugging Face
            ├─ Execute inference with MLX
            └─ Stream tokens back as JSON-Lines
```

## Installation

### Requirements
- Python 3.10+
- macOS with Apple Silicon (M1/M2/M3)
- MLX and mlx-lm packages

### Setup

```bash
# Create Python virtual environment
python3 -m venv ~/.ollama/mlx-env

# Activate environment
source ~/.ollama/mlx-env/bin/activate

# Install dependencies
pip install -r mlx_backend/requirements.txt
```

## API Endpoints

### POST /completion

Handles text generation requests with streaming.

**Request Format** (matches Ollama's `CompletionRequest`):
```json
{
  "prompt": "What is machine learning?",
  "images": [],
  "format": null,
  "options": {
    "temperature": 0.7,
    "top_k": 40,
    "top_p": 0.9,
    "num_predict": 128,
    "repeat_penalty": 1.1,
    "repeat_last_n": 64,
    "presence_penalty": 0.0,
    "frequency_penalty": 0.0
  },
  "grammar": "",
  "shift": false,
  "truncate": false,
  "logprobs": false,
  "top_logprobs": 0
}
```

**Response Format** (streaming JSON-Lines, matches Ollama's `CompletionResponse`):
```json
{"content": "Machine", "done": false, "eval_count": 1, "eval_duration": 123456789, "prompt_eval_count": 5, "prompt_eval_duration": 987654321}
{"content": " learning", "done": false, "eval_count": 2, "eval_duration": 123456789, "prompt_eval_count": 5, "prompt_eval_duration": 987654321}
{"content": " is", "done": true, "done_reason": "stop", "eval_count": 3, "eval_duration": 123456789, "prompt_eval_count": 5, "prompt_eval_duration": 987654321}
```

### POST /load

Load a model into memory.

**Request:**
```json
{
  "model": "meta-llama/Llama-2-7b",
  "stream": false
}
```

**Response:**
```json
{
  "status": "loaded",
  "model": "meta-llama/Llama-2-7b",
  "parameters": {}
}
```

### GET /health

Health check endpoint.

**Response:**
```json
{
  "status": "ok",
  "model_loaded": true,
  "current_model": "meta-llama/Llama-2-7b"
}
```

### GET /info

Server info (GPU devices, compute capability).

**Response:**
```json
{
  "gpu": "MLX (Apple Silicon)",
  "compute_capability": "Metal Performance Shaders",
  "device": "Apple Neural Engine"
}
```

## Running the Server

### Manual Start

```bash
# Activate environment
source ~/.ollama/mlx-env/bin/activate

# Start server on specific port
python -m mlx_backend.server --port 8001 --host 127.0.0.1
```

### As Ollama Subprocess

The Ollama server will automatically start this backend as a subprocess when using MLX models. Configuration is handled by the Go layer.

## Testing

```bash
# In one terminal, start the server
python -m mlx_backend.server --port 8000

# In another terminal, run tests
python mlx_backend/test_server.py
```

## Model Management

### Model Discovery

Models are loaded from Hugging Face Hub. MLX models are typically found in:
- `meta-llama/*` - Meta's Llama models
- `mistralai/*` - Mistral models
- `HuggingFaceTB/*` - HuggingFace proprietary models

### Model Cache

Downloaded models are cached in:
```
~/.ollama/models/mlx/
```

### Supported Models

Models must be available in MLX format on Hugging Face. Some popular options:
- `meta-llama/Llama-2-7b`
- `mistralai/Mistral-7B`
- `HuggingFaceTB/SmolLM`

## Performance Characteristics

### Advantages over GGUF
- Native utilization of Apple Silicon's unified memory
- Automatic GPU/CPU offloading via MLX runtime
- Better performance on M-series Macs for comparable models

### Considerations
- Quantized models may have different optimal configurations than GGUF
- Some GGUF-only models may not have MLX equivalents
- First load includes model download from Hugging Face (requires internet)

## Debugging

### Verbose Logging

```bash
python -m mlx_backend.server --log-level debug
```

### Manual Request Testing

```bash
curl -X POST http://127.0.0.1:8000/completion \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Hello",
    "options": {"num_predict": 10}
  }'
```

## Integration with Ollama Server

The Go layer in `llm/server.go` handles:
1. Starting the MLX backend subprocess on a dynamic port
2. Marshaling `CompletionRequest` to JSON
3. Sending HTTP POST to the backend
4. Unmarshaling `CompletionResponse` from streaming JSON
5. Forwarding to the client

No changes to the HTTP API are needed—all endpoints work identically.

## Future Improvements

- [ ] Support for vision models (multimodal)
- [ ] LoRA fine-tuning support
- [ ] Function calling / tool use
- [ ] Batch inference optimization
- [ ] Model quantization utilities
- [ ] Extended context length support

## License

Same as Ollama (MIT)
