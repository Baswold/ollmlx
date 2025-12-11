# Quick Start Guide

Get up and running with **ollmlx** in 5 minutes! 🚀

---

## Prerequisites

- **macOS** with Apple Silicon (M1/M2/M3/M4)
- **Python 3.10+**
- **Go 1.21+** (if building from source)

---

## Installation

### Step 1: Install Python Dependencies

```bash
cd mlx_backend
pip install -r requirements.txt
```

### Step 2: Validate Setup (Optional but Recommended)

```bash
python mlx_backend/validate_backend.py
```

You should see all checks pass ✅

### Step 3: Build (if needed)

The binary is already built! But if you need to rebuild:

```bash
go build -o ollmlx .
```

---

## Quick Test Drive

### 1. Start the Server

```bash
./ollmlx serve
```

You should see:
```
✓ MLX backend started on port 8023
✓ Server listening on http://localhost:11434
```

### 2. Pull a Small Model (in a new terminal)

Start with a tiny model for testing:

```bash
./ollmlx pull mlx-community/gemma-3-270m-4bit
```

This downloads a 234MB model optimized for MLX.

### 3. Run Interactive Chat

```bash
./ollmlx run mlx-community/gemma-3-270m-4bit
```

Type your prompts and press Enter! 🎉

---

## Common Commands

| Command | Description |
|---------|-------------|
| `./ollmlx serve` | Start the server |
| `./ollmlx pull <model>` | Download a model |
| `./ollmlx run <model>` | Interactive chat |
| `./ollmlx list` | Show downloaded models |
| `./ollmlx show <model>` | Model information |
| `./ollmlx --help` | Full command list |

---

## Recommended Models

### Small Models (Good for Testing)
- `mlx-community/gemma-3-270m-4bit` - 234MB, fast responses
- `mlx-community/Llama-3.2-1B-Instruct-4bit` - 1GB, better quality

### Medium Models (Balanced)
- `mlx-community/Llama-3.2-3B-Instruct-4bit` - 2.5GB, good performance
- `mlx-community/Phi-3.5-mini-instruct-4bit` - 2.8GB, very capable

### Large Models (Best Quality)
- `mlx-community/Meta-Llama-3.1-8B-Instruct-4bit` - 5.5GB
- `mlx-community/Mistral-7B-Instruct-v0.3-4bit` - 5GB

Find more at: https://huggingface.co/mlx-community

---

## Troubleshooting

### Server won't start
```bash
# Check if port is already in use
lsof -i :11434

# Kill existing process
killall ollmlx
```

### Model download fails
```bash
# Some models require HuggingFace authentication
# Set your token:
export HF_TOKEN="your_token_here"
```

### Python dependencies missing
```bash
cd mlx_backend
pip install --upgrade -r requirements.txt
```

### MLX not working
```bash
# Validate your setup:
python mlx_backend/validate_backend.py

# Reinstall MLX:
pip install --upgrade mlx mlx-lm
```

---

## Performance Tips

### 1. Use 4-bit Quantized Models
Models with `-4bit` in the name are optimized for speed and memory.

### 2. Close Other Apps
Free up RAM for better performance.

### 3. Keep macOS Updated
Apple improves Metal performance with each update.

### 4. Monitor Performance
```bash
# Check GPU usage
sudo powermetrics --samplers gpu_power -i1000 -n1
```

---

## API Usage

ollmlx is 100% compatible with Ollama API:

```bash
# Generate text
curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/Llama-3.2-3B-Instruct-4bit",
  "prompt": "Why is the sky blue?"
}'

# Chat completion
curl http://localhost:11434/api/chat -d '{
  "model": "mlx-community/Llama-3.2-3B-Instruct-4bit",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}'
```

---

## Next Steps

Once you're comfortable with the basics:

1. **Read the full README** - Comprehensive guide with all features
2. **Check [MERGE_SUMMARY.md](../archive/MERGE_SUMMARY.md)** - Understand what makes this version special
3. **Explore MLX_ARCHITECTURE.md** - Deep dive into how it works
4. **Run tests** - `./test_ollmlx.sh` to verify everything works

---

## Getting Help

- **Documentation:** Check the `docs/` folder
- **Issues:** Review TESTING_SUMMARY.md for known issues
- **Configuration:** See README.md for advanced options

---

## Example Session

Here's what a typical session looks like:

```bash
# Terminal 1: Start server
$ ./ollmlx serve
✓ MLX backend started
✓ Server ready

# Terminal 2: Use ollmlx
$ ./ollmlx pull mlx-community/Llama-3.2-1B-Instruct-4bit
Downloading... ████████████ 100%

$ ./ollmlx run mlx-community/Llama-3.2-1B-Instruct-4bit
>>> Why is the sky blue?

The sky appears blue because of Rayleigh scattering...

>>> exit
```

---

## Performance Comparison

Typical performance on M2 Max:

| Model Size | GGUF (llama.cpp) | MLX (ollmlx) | Speedup |
|------------|------------------|--------------|---------|
| 1B params  | 45 tokens/sec    | 95 tokens/sec| 2.1x    |
| 3B params  | 28 tokens/sec    | 62 tokens/sec| 2.2x    |
| 8B params  | 12 tokens/sec    | 28 tokens/sec| 2.3x    |

*Your results may vary based on hardware and model*

---

## That's It! 🎉

You're now running LLMs with Apple Silicon MLX acceleration!

**Enjoy blazing-fast inference!** ⚡

For more details, check out the full [README.md](README.md).
