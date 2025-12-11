# ollmlx Quick Start - Simple Version

**Get started with ollmlx in 3 easy steps!**

## 1️⃣ Install

```bash
# Clone and build
git clone https://github.com/ollama/ollama.git
cd ollama
go build -o ollmlx .

# Install Python dependencies
pip install -r mlx_backend/requirements.txt
```

## 2️⃣ Run

```bash
# Start the server
./ollmlx serve &

# In another terminal, pull a model
./ollmlx pull gemma2:2b

# Chat with the model!
./ollmlx run gemma2:2b
```

## 3️⃣ Use the API

```bash
# Generate text
curl http://localhost:11434/api/generate -d '{
  "model": "gemma2:2b",
  "prompt": "Why is the sky blue?"
}'

# Chat completion
curl http://localhost:11434/api/chat -d '{
  "model": "gemma2:2b",
  "messages": [{"role": "user", "content": "Hello!"}]
}'
```

## 🎉 That's it!

**For more details:** See [QUICKSTART.md](QUICKSTART.md)

**Need help?** Check [TESTING_GUIDE.md](TESTING_GUIDE.md)

**Found an issue?** See [test_results_mlx_generation.md](../archive/test_results_mlx_generation.md) for known limitations.