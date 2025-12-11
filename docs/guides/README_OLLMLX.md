# ollmlx - MLX-Powered Ollama Fork

**🚀 Apple Silicon Optimized** | **✨ 100% Ollama Compatible** | **🔄 MLX + GGUF Support**

ollmlx is a fork of Ollama that **replaces the GGUF/llama.cpp inference layer with MLX** while maintaining 100% compatibility with Ollama's API and CLI.

## What is ollmlx?

ollmlx automatically detects model formats and routes to the optimal backend for your hardware:

- **MLX models** → Apple's MLX framework (Metal Performance Shaders on Apple Silicon)
- **GGUF models** → llama.cpp (standard Ollama backend)

### Why ollmlx?

**Performance**: MLX is specifically optimized for Apple Silicon, delivering faster inference and better memory utilization than GGUF on M1/M2/M3 Macs.

**Compatibility**: Works with all existing Ollama clients—GitHub Copilot, VSCode extensions, LangChain, and any tool that uses the Ollama API.

**Simplicity**: No configuration needed. Just pull an MLX model and ollmlx handles the rest.

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/YourUsername/Ollama-MLX.git
cd Ollama-MLX

# Build ollmlx
go build .

# Install MLX backend dependencies
pip install -r mlx_backend/requirements.txt

# Run the server
./ollama serve
```

> **Tip:** ollmlx stores MLX caches alongside standard models under `$(OLLAMA_MODELS)/mlx` (defaults to `~/.ollama/models/mlx`). Set `OLLAMA_MODELS=/custom/path` to relocate both GGUF and MLX caches.

### Pull an MLX Model

```bash
# Pull a small MLX model from HuggingFace
ollama pull mlx-community/Llama-3.2-1B-Instruct-4bit
```

### Run the Model

```bash
# Chat with the MLX model
ollama run mlx-community/Llama-3.2-1B-Instruct-4bit
```

### Use via API

```bash
# Generate text
curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/Llama-3.2-1B-Instruct-4bit",
  "prompt": "Why is the sky blue?"
}'
```

## Supported MLX Models

Popular MLX models from HuggingFace (all use `mlx-community/` prefix):

| Model                          | Parameters | Size   | Command                                               |
| ------------------------------ | ---------- | ------ | ----------------------------------------------------- |
| Llama 3.2 Instruct (4-bit)     | 1B         | ~750MB | `ollama pull mlx-community/Llama-3.2-1B-Instruct-4bit`|
| Llama 3.2 Instruct (4-bit)     | 3B         | ~2GB   | `ollama pull mlx-community/Llama-3.2-3B-Instruct-4bit`|
| Mistral 7B Instruct (4-bit)    | 7B         | ~4GB   | `ollama pull mlx-community/Mistral-7B-Instruct-v0.3-4bit`|
| Qwen 2.5 Instruct (4-bit)      | 7B         | ~4GB   | `ollama pull mlx-community/Qwen2.5-7B-Instruct-4bit`  |
| Phi-3.5 Mini Instruct (4-bit)  | 3.8B       | ~2.3GB | `ollama pull mlx-community/Phi-3.5-mini-instruct-4bit`|
| Gemma 2 IT (4-bit)             | 2B         | ~1.5GB | `ollama pull mlx-community/gemma-2-2b-it-4bit`        |
| SmolLM2 Instruct (4-bit)       | 1.7B       | ~1GB   | `ollama pull mlx-community/SmolLM2-1.7B-Instruct-4bit`|

Browse all MLX models: https://huggingface.co/mlx-community

For ad-hoc downloads, references ending in `-mlx` (e.g., `gemma-3-270m-4bit`) are auto-detected as MLX and stored under `$(OLLAMA_MODELS)/mlx`.

## Feature Comparison

| Feature                     | ollmlx         | Standard Ollama |
| --------------------------- | -------------- | --------------- |
| GGUF Models                 | ✅              | ✅               |
| MLX Models                  | ✅              | ❌               |
| Apple Silicon Optimized     | ✅ (via MLX)    | Partial         |
| Auto Model Format Detection | ✅              | N/A             |
| HuggingFace Integration     | ✅              | ❌               |
| Ollama API Compatible       | ✅ 100%         | ✅               |
| IDE Extensions Compatible   | ✅              | ✅               |

## Architecture

ollmlx extends Ollama's architecture with MLX support:

```
┌─────────────────────────────────────┐
│       Ollama HTTP API               │
│   /api/generate, /api/chat, etc.    │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│    Model Format Detection           │
│   (GGUF vs MLX)                     │
└──────────────┬──────────────────────┘
               │
        ┌──────┴──────┐
        │             │
        ▼             ▼
    [GGUF]        [MLX]
    llama.cpp     MLX Backend (Python)
    ├─ CPU/GPU    ├─ Metal Performance Shaders
    └─ Standard   └─ Apple Silicon Optimized
```

See [docs/MLX_ARCHITECTURE.md](docs/MLX_ARCHITECTURE.md) for detailed architecture documentation.

## Examples

### Python (Ollama Library)

```python
import ollama

# Pull and use MLX model
ollama.pull('mlx-community/Llama-3.2-1B-Instruct-4bit')

response = ollama.chat(
    model='mlx-community/Llama-3.2-1B-Instruct-4bit',
    messages=[{'role': 'user', 'content': 'Hello!'}]
)
print(response['message']['content'])
```

### Bash

```bash
# List all models (both GGUF and MLX)
ollama list

# Show model details
ollama show mlx-community/Llama-3.2-1B-Instruct-4bit

# Delete model
ollama rm mlx-community/Llama-3.2-1B-Instruct-4bit
```

More examples in [`examples/mlx/`](examples/mlx/)

## Documentation

- [MLX Architecture](docs/MLX_ARCHITECTURE.md) - Technical design and implementation details
- [Supported Models](docs/SUPPORTED_MODELS.md) - Tested MLX models and performance data
- [Migration Guide](docs/MIGRATION_FROM_OLLAMA.md) - Switching from standard Ollama
- [Ollama Documentation](https://docs.ollama.com) - Standard Ollama features (all work with ollmlx)

## Requirements

### For MLX Models
- **Apple Silicon Mac** (M1, M2, M3, or later)
- **macOS 13.3+**
- **Python 3.9+**
- **MLX framework** (`pip install mlx-lm`)

### For GGUF Models
- Same requirements as standard Ollama
- Works on all platforms (macOS, Linux, Windows)

## Performance

On Apple Silicon, MLX models typically show:
- **20-40% faster** token generation vs GGUF
- **Better memory efficiency** due to unified memory architecture
- **Lower power consumption** thanks to Metal Performance Shaders

Benchmarks available in [docs/SUPPORTED_MODELS.md](docs/SUPPORTED_MODELS.md)

## Development

```bash
# Build
go build .

# Run tests
go test ./...
go test ./test/mlx_integration_test.go

# Test MLX backend
cd mlx_backend
python test_server.py
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Areas where help is appreciated:
- Testing on different Apple Silicon models (M1/M2/M3/M4)
- Adding support for more MLX model architectures
- Performance benchmarking
- Documentation improvements

## Limitations

- MLX backend requires Apple Silicon and macOS
- Not all Ollama models have MLX equivalents (see [Supported Models](docs/SUPPORTED_MODELS.md))
- MLX model quantization strategies differ from GGUF

## FAQ

**Q: Does this replace standard Ollama?**
A: No, ollmlx is a drop-in replacement that adds MLX support. Use it on Apple Silicon for better performance, or use standard Ollama on other platforms.

**Q: Can I use both GGUF and MLX models?**
A: Yes! ollmlx automatically detects the format and uses the appropriate backend.

**Q: Will my IDE extensions still work?**
A: Yes, ollmlx is 100% API-compatible with Ollama. All existing tools and integrations work without modification.

**Q: How do I convert a GGUF model to MLX?**
A: Most popular models already have MLX versions on HuggingFace under `mlx-community/`. For custom models, see [MLX documentation](https://github.com/ml-explore/mlx).

**Q: What about upstream Ollama updates?**
A: ollmlx can be periodically rebased with upstream Ollama to incorporate new features and fixes.

## License

ollmlx maintains Ollama's MIT License. See [LICENSE](LICENSE) for details.

## Acknowledgments

- **Ollama Team** - For creating the excellent Ollama framework
- **Apple MLX Team** - For the MLX framework and optimizations
- **HuggingFace mlx-community** - For providing converted MLX models

## Related Projects

- [Ollama](https://github.com/ollama/ollama) - The original project
- [MLX](https://github.com/ml-explore/mlx) - Apple's ML framework
- [MLX Examples](https://github.com/ml-explore/mlx-examples) - MLX usage examples

---

**Note**: This is an unofficial fork focused on Apple Silicon optimization. For general use, please see the official [Ollama project](https://github.com/ollama/ollama).
