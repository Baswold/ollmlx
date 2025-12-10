# Migration Guide: From Ollama to ollmlx

This guide helps you migrate from standard Ollama to ollmlx and start using MLX models on Apple Silicon.

## TL;DR

ollmlx is a **drop-in replacement** for Ollama. If you're on Apple Silicon:

```bash
# Install ollmlx
git clone https://github.com/YourUsername/Ollama-MLX.git
cd Ollama-MLX
go build .
pip install -r mlx_backend/requirements.txt

# Your existing models and workflows continue to work
./ollama list  # Shows both GGUF and MLX models
./ollama run llama3.2  # GGUF model (works as before)

# New: Pull and use MLX models
./ollama pull mlx-community/Llama-3.2-3B-Instruct-4bit
./ollama run mlx-community/Llama-3.2-3B-Instruct-4bit
```

## Should You Migrate?

### Migrate if you:
✅ Use **Apple Silicon** (M1, M2, M3, M4)
✅ Want **faster inference** on Mac
✅ Need **better memory efficiency**
✅ Want access to **HuggingFace MLX models**
✅ Are comfortable with **Python + Go toolchain**

### Stick with Ollama if you:
❌ Use **Intel Mac, Linux, or Windows**
❌ Prefer **official/stable releases only**
❌ Don't want to **manage Python dependencies**
❌ Only use **GGUF models** from Ollama library

## Installation

### Prerequisites

**Verify you have Apple Silicon**:
```bash
uname -m
# Should output: arm64
```

**Check macOS version**:
```bash
sw_vers
# Should be macOS 13.3 or later
```

### Step 1: Install Dependencies

```bash
# Install Homebrew (if needed)
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# Install Go (if needed)
brew install go

# Install Python 3.9+ (if needed)
brew install python@3.11
```

### Step 2: Build ollmlx

```bash
# Clone the repository
git clone https://github.com/YourUsername/Ollama-MLX.git
cd Ollama-MLX

# Build the binary
go build .

# Verify build
./ollama --version
```

### Step 3: Install MLX Backend

```bash
# Create Python virtual environment (recommended)
python3 -m venv venv
source venv/bin/activate

# Install MLX dependencies
pip install -r mlx_backend/requirements.txt

# Verify MLX installation
python -c "import mlx; print('MLX installed successfully')"
```

### Step 4: Run ollmlx Server

```bash
# Start the server
./ollama serve

# In another terminal, test it
./ollama list
```

## Migrating Existing Models

### Your GGUF Models Continue to Work

All your existing Ollama models (GGUF format) work with ollmlx:

```bash
# If you had Ollama installed before
ls ~/.ollama/models/manifests/

# ollmlx uses the same directory structure
./ollama list  # Shows all your existing GGUF models
./ollama run llama3.2  # Works exactly as before
```

**No migration needed for GGUF models!**

### Finding MLX Equivalents

Most popular Ollama models have MLX versions:

| Ollama Model | MLX Equivalent | Command |
|--------------|----------------|---------|
| `llama3.2` | `mlx-community/Llama-3.2-3B-Instruct-4bit` | `ollama pull mlx-community/Llama-3.2-3B-Instruct-4bit` |
| `llama3.2:1b` | `mlx-community/Llama-3.2-1B-Instruct-4bit` | `ollama pull mlx-community/Llama-3.2-1B-Instruct-4bit` |
| `mistral` | `mlx-community/Mistral-7B-Instruct-v0.3-4bit` | `ollama pull mlx-community/Mistral-7B-Instruct-v0.3-4bit` |
| `qwen2.5:7b` | `mlx-community/Qwen2.5-7B-Instruct-4bit` | `ollama pull mlx-community/Qwen2.5-7B-Instruct-4bit` |
| `phi3.5` | `mlx-community/Phi-3.5-mini-instruct-4bit` | `ollama pull mlx-community/Phi-3.5-mini-instruct-4bit` |

**Search for more**: https://huggingface.co/mlx-community

### Comparing Performance

Test both versions to see the speed difference:

```bash
# Test GGUF version
time ./ollama run llama3.2:3b "Explain quantum computing"

# Test MLX version
time ./ollama run mlx-community/Llama-3.2-3B-Instruct-4bit "Explain quantum computing"
```

On Apple Silicon, MLX is typically 20-40% faster.

## Migrating Workflows

### CLI Commands (100% Compatible)

All Ollama CLI commands work identically:

```bash
# List models (now includes MLX)
ollama list

# Pull models (auto-detects MLX vs GGUF)
ollama pull llama3.2  # GGUF from Ollama registry
ollama pull mlx-community/Llama-3.2-3B-Instruct-4bit  # MLX from HuggingFace

# Run models
ollama run <model-name>

# Delete models
ollama rm <model-name>

# Show model info
ollama show <model-name>
```

### API Endpoints (100% Compatible)

If you're using Ollama's HTTP API, **nothing changes**:

```bash
# Generate text (works for both GGUF and MLX)
curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/Llama-3.2-3B-Instruct-4bit",
  "prompt": "Why is the sky blue?"
}'

# Chat endpoint
curl http://localhost:11434/api/chat -d '{
  "model": "mlx-community/Llama-3.2-3B-Instruct-4bit",
  "messages": [{"role": "user", "content": "Hello!"}]
}'

# List models
curl http://localhost:11434/api/tags
```

### Python Client (100% Compatible)

If you use the Ollama Python library:

```python
import ollama

# No code changes needed!
ollama.pull('mlx-community/Llama-3.2-1B-Instruct-4bit')

response = ollama.chat(
    model='mlx-community/Llama-3.2-1B-Instruct-4bit',
    messages=[{'role': 'user', 'content': 'Hello!'}]
)
print(response['message']['content'])
```

### JavaScript Client (100% Compatible)

```javascript
import ollama from 'ollama'

// Works the same way
const response = await ollama.chat({
  model: 'mlx-community/Llama-3.2-1B-Instruct-4bit',
  messages: [{ role: 'user', content: 'Hello!' }],
})
console.log(response.message.content)
```

## IDE Extensions & Tools

### GitHub Copilot

If you use Ollama as a GitHub Copilot backend:

```json
// settings.json - No changes needed
{
  "github.copilot.advanced": {
    "model": "mlx-community/Llama-3.2-3B-Instruct-4bit"
  }
}
```

### VSCode Extensions

All VSCode extensions that work with Ollama work with ollmlx:

- **Continue**: No changes
- **Ollama**: No changes
- **Code GPT**: No changes

Just update model name to MLX model.

### LangChain

```python
from langchain_community.llms import Ollama

# Works with MLX models
llm = Ollama(model="mlx-community/Llama-3.2-3B-Instruct-4bit")
response = llm("Explain machine learning")
```

### LlamaIndex

```python
from llama_index.llms.ollama import Ollama

# Works with MLX models
llm = Ollama(model="mlx-community/Llama-3.2-3B-Instruct-4bit")
```

## Configuration

### Model Storage

ollmlx uses Ollama's directory structure with MLX models in a separate subdirectory:

```
~/.ollama/
├── models/
│   ├── manifests/       # GGUF models (Ollama standard)
│   ├── blobs/           # GGUF model files
│   └── mlx/             # MLX models (new)
│       ├── mlx-community_Llama-3.2-1B-Instruct-4bit/
│       │   ├── config.json
│       │   ├── model.safetensors
│       │   └── tokenizer.json
│       └── ...
```

### Environment Variables

All Ollama environment variables continue to work:

```bash
# GPU configuration (for GGUF models)
export OLLAMA_NUM_GPU=1

# Logging
export OLLAMA_DEBUG=1

# Host/port
export OLLAMA_HOST=0.0.0.0:11434
```

New MLX-specific variables (optional):

```bash
# Python path for MLX backend
export OLLAMA_MLX_PYTHON=/path/to/python3

# MLX model cache directory (default: ~/.ollama/models/mlx)
export OLLAMA_MLX_CACHE=~/.cache/mlx-models
```

## Troubleshooting

### MLX Backend Not Starting

**Symptom**: Models fail to load with MLX error

**Solution**:
```bash
# Test MLX backend directly
cd mlx_backend
python server.py --port 9090

# Check Python dependencies
pip install --upgrade mlx mlx-lm fastapi uvicorn

# Verify MLX works
python -c "import mlx; print(mlx.__version__)"
```

### Models Download Slowly

**Symptom**: `ollama pull` for MLX models is slow

**Solution**:
- MLX models download from HuggingFace (may be slower than Ollama registry)
- Use a wired connection if possible
- Consider downloading large models during off-peak hours
- Check HuggingFace status: https://status.huggingface.co/

### Out of Memory Errors

**Symptom**: Model loading fails with OOM error

**Solution**:
```bash
# Use smaller models
ollama pull mlx-community/Llama-3.2-1B-Instruct-4bit  # Instead of 3B

# Close other apps
# Use Activity Monitor to free up RAM

# Check model size first
ollama show mlx-community/<model-name> | grep size
```

### Performance Not Improved

**Symptom**: MLX models not faster than GGUF

**Checklist**:
- ✅ Verify you're on Apple Silicon: `uname -m` → `arm64`
- ✅ Check model is actually MLX: `ollama show <model> | grep format`
- ✅ Close background apps (frees GPU resources)
- ✅ Use 4-bit quantized models for best speed
- ✅ First generation is slower (model loading), subsequent ones are fast

## Reverting to Standard Ollama

If you need to go back to standard Ollama:

```bash
# Stop ollmlx server
pkill ollama

# Reinstall standard Ollama
brew install ollama

# Start standard Ollama
ollama serve

# Your GGUF models still work
# MLX models won't load (ignored)
```

Your GGUF models are in the standard location and will continue working with standard Ollama.

## Best Practices

### Model Management

```bash
# Keep only models you use
ollama list
ollama rm <unused-models>

# For storage: GGUF models are usually smaller
# For speed: MLX models are faster on Apple Silicon

# Good strategy:
# - Small/frequent models: MLX (fast, always loaded)
# - Large/occasional models: GGUF (smaller storage)
```

### Development Workflow

```bash
# Use fast small models for development
ollama pull mlx-community/SmolLM2-135M-Instruct-4bit

# Use larger models for production/quality
ollama pull mlx-community/Llama-3.2-3B-Instruct-4bit
```

### Memory Management

```bash
# Monitor memory usage
# Activity Monitor → Memory tab

# Recommended RAM:
# - 8GB: Up to 1B models
# - 16GB: Up to 3-7B models
# - 32GB: Up to 13B models
# - 64GB+: Any model
```

## Getting Help

### Common Resources

- **ollmlx GitHub Issues**: Bug reports and feature requests
- **Ollama Discord**: General Ollama questions (most apply to ollmlx)
- **MLX GitHub**: MLX framework issues

### Reporting Issues

When reporting issues, include:

```bash
# System info
uname -a
sw_vers

# ollmlx version
./ollama --version

# MLX version
python -c "import mlx; print(mlx.__version__)"

# Model being used
ollama show <model-name>

# Error logs
tail -n 50 ~/.ollama/logs/server.log
```

## What's Next?

After migrating:

1. **Benchmark**: Test MLX vs GGUF for your use case
2. **Explore**: Try different MLX models from HuggingFace
3. **Optimize**: Find the best model for your hardware
4. **Contribute**: Share benchmarks and feedback

## FAQ

**Q: Can I use both ollmlx and standard Ollama?**
A: Not simultaneously (same port). But you can switch between them. They share GGUF model storage.

**Q: Will this break my existing Ollama setup?**
A: No. ollmlx uses the same directory structure. Your GGUF models work with both.

**Q: Do I need to redownload all my models?**
A: No for GGUF models. Yes for MLX models (they're a different format).

**Q: Is ollmlx officially supported?**
A: No, this is an unofficial fork. Use at your own discretion.

**Q: Can I contribute to ollmlx?**
A: Yes! See CONTRIBUTING.md for guidelines.

---

**Welcome to ollmlx!** 🚀

Enjoy faster local LLM inference on your Apple Silicon Mac.
