# Quick Test Guide for Mac

This guide helps you verify ollmlx is working correctly on your Mac.

## Prerequisites

- macOS 14.0+ (Sonoma or later)
- Apple Silicon Mac (M1/M2/M3/M4)
- Python 3.10+
- Go 1.21+

## Quick Start (5 minutes)

### 1. Install ollmlx

```bash
# Clone and build
git clone https://github.com/ollama/ollama.git ollmlx
cd ollmlx

# Run the installer (sets up Python venv, builds binary)
./scripts/install_ollmlx.sh
```

### 2. Verify Installation

```bash
# Check everything is set up correctly
./ollmlx doctor
```

You should see all green checkmarks.

### 3. Start the Server

```bash
# Start ollmlx in the background
./ollmlx serve &
```

### 4. Pull a Test Model

```bash
# Pull a small, fast Gemma model (about 1.5GB)
./ollmlx pull mlx-community/gemma-2-2b-it-4bit

# Or try an even smaller model for quick testing (~150MB)
./ollmlx pull mlx-community/SmolLM2-135M-Instruct-4bit
```

### 5. Test It!

```bash
# Interactive chat
./ollmlx run mlx-community/gemma-2-2b-it-4bit

# Or test via API
curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/gemma-2-2b-it-4bit",
  "prompt": "Hello! What is 2+2?",
  "stream": false
}'
```

## Automated Test Script

For a comprehensive test, run:

```bash
./scripts/test_gemma_mlx.sh
```

This will:
1. Start the server (if not running)
2. Pull the Gemma model
3. Test generation endpoint
4. Test streaming
5. Test chat endpoint
6. Report performance metrics

## Alternative Models to Try

| Model | Size | Speed | Use Case |
|-------|------|-------|----------|
| `mlx-community/SmolLM2-135M-Instruct-4bit` | 150MB | Very Fast | Quick tests |
| `mlx-community/gemma-2-2b-it-4bit` | 1.5GB | Fast | General use |
| `mlx-community/Llama-3.2-1B-Instruct-4bit` | 750MB | Fast | Coding, chat |
| `mlx-community/Llama-3.2-3B-Instruct-4bit` | 2GB | Medium | Better quality |
| `mlx-community/Mistral-7B-Instruct-v0.3-4bit` | 4GB | Medium | High quality |

## Troubleshooting

### Server won't start

```bash
# Check if something else is using port 11434
lsof -i :11434

# Kill existing process if needed
pkill -f "ollmlx serve"

# Try starting with verbose logging
OLLAMA_DEBUG=1 ./ollmlx serve
```

### Model pull fails

```bash
# Check internet connection
curl -I https://huggingface.co

# For gated models, login first
./ollmlx login
```

### Slow performance

```bash
# Check Metal is being used
# Look for "Using device: mps" in logs

# Make sure you're using MLX models (not GGUF)
./ollmlx list
```

### Python errors

```bash
# Reinstall Python dependencies
source ~/.ollmlx/venv/bin/activate
pip install -r mlx_backend/requirements.txt
```

## Expected Performance

On Apple Silicon, you should see:
- **M1/M2 Base**: 30-50 tokens/second
- **M1/M2 Pro/Max**: 50-80 tokens/second
- **M3 Pro/Max**: 60-100 tokens/second
- **M4 Pro/Max**: 80-120+ tokens/second

Performance varies by model size and quantization.

## Success Criteria

Your test is successful if:
- [x] `ollmlx doctor` shows all green
- [x] `ollmlx pull` downloads a model
- [x] `ollmlx run` starts an interactive chat
- [x] API calls return valid responses
- [x] Tokens generate at reasonable speed (>20 tok/s)

## Need Help?

- Run `./ollmlx doctor` for diagnostics
- Check logs with `OLLAMA_DEBUG=1`
- Open an issue on GitHub
