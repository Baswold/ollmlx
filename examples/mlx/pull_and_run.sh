#!/bin/bash
# Example: Pull and run an MLX model with ollmlx
#
# This demonstrates how to use ollmlx to pull and run MLX models
# from HuggingFace, maintaining full compatibility with Ollama's API.

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== ollmlx MLX Model Example ===${NC}"
echo ""

# Example 1: Pull a small MLX model
echo -e "${GREEN}1. Pulling MLX model from HuggingFace...${NC}"
echo "Model: mlx-community/Llama-3.2-1B-Instruct-4bit"
echo ""

curl http://localhost:11434/api/pull -d '{
  "name": "mlx-community/Llama-3.2-1B-Instruct-4bit"
}'

echo ""
echo ""

# Example 2: List all models (includes both GGUF and MLX)
echo -e "${GREEN}2. Listing all available models...${NC}"
echo ""

curl http://localhost:11434/api/tags | jq '.models[] | {name: .name, format: .details.format, size: .size}'

echo ""
echo ""

# Example 3: Generate text with MLX model
echo -e "${GREEN}3. Generating text with MLX model...${NC}"
echo ""

curl http://localhost:11434/api/generate -d '{
  "model": "mlx-community/Llama-3.2-1B-Instruct-4bit",
  "prompt": "Why is the sky blue?",
  "stream": false
}' | jq '.response'

echo ""
echo ""

# Example 4: Show model details
echo -e "${GREEN}4. Showing MLX model details...${NC}"
echo ""

curl http://localhost:11434/api/show -d '{
  "name": "mlx-community/Llama-3.2-1B-Instruct-4bit"
}' | jq '{format: .details.format, family: .details.family, size: .size}'

echo ""
echo ""
echo -e "${BLUE}=== Example Complete ===${NC}"
echo ""
echo "You can now use any Ollama-compatible client with MLX models!"
echo "Try these popular MLX models:"
echo "  - mlx-community/Llama-3.2-3B-Instruct-4bit"
echo "  - mlx-community/Mistral-7B-Instruct-v0.3-4bit"
echo "  - mlx-community/Qwen2.5-7B-Instruct-4bit"
