# ollmlx Quick Start - Simple Version

**Get started with ollmlx in 3 easy steps!**

## Prerequisites

- macOS 14.0+ (Sonoma or later)
- Apple Silicon Mac (M1/M2/M3/M4)
- Python 3.10+
- Go 1.21+

## 1. Install

```bash
# Clone and build (recommended)
git clone https://github.com/ollama/ollama.git ollmlx
cd ollmlx
./scripts/install_ollmlx.sh

# Or build manually
go build -o ollmlx .
pip install -r mlx_backend/requirements.txt
```

## 2. Run

```bash
# Start the server
./ollmlx serve &

# Pull an MLX model (fast, optimized for Apple Silicon)
./ollmlx pull mlx-community/gemma-2-2b-it-4bit

# Chat with the model!
./ollmlx run mlx-community/gemma-2-2b-it-4bit
```

## 3. Use the API

```bash
# Generate text
curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/gemma-2-2b-it-4bit",
  "prompt": "Why is the sky blue?",
  "stream": false
}'

# Chat completion
curl http://localhost:11434/api/chat -d '{
  "model": "mlx-community/gemma-2-2b-it-4bit",
  "messages": [{"role": "user", "content": "Hello!"}],
  "stream": false
}'
```

## That's it!

### Quick Model Recommendations

| Model | Size | Best For |
|-------|------|----------|
| `mlx-community/SmolLM2-135M-Instruct-4bit` | 150MB | Quick testing |
| `mlx-community/gemma-2-2b-it-4bit` | 1.5GB | General use |
| `mlx-community/Llama-3.2-1B-Instruct-4bit` | 750MB | Coding, chat |
| `mlx-community/Mistral-7B-Instruct-v0.3-4bit` | 4GB | High quality |

### Run Diagnostics

```bash
./ollmlx doctor
```

### Run Tests

```bash
# Quick integration test
./scripts/test_gemma_mlx.sh
```

**For more details:** See [QUICKSTART_DETAILED.md](QUICKSTART_DETAILED.md)

**Test Guide:** See [TEST_ON_MAC.md](../../TEST_ON_MAC.md)

**Architecture:** See [MLX_ARCHITECTURE.md](../MLX_ARCHITECTURE.md)