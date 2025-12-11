#!/bin/bash

echo "=== Debugging MLX Generation ==="
echo ""

# Test 1: Check if model is detected as MLX
echo "1. Testing MLX model detection..."
MODEL="mlx-community/gemma-3-270m-4bit"
echo "   Model: $MODEL"

# Test if the model exists in the MLX cache
if ./ollmlx list | grep -q "mlx-community_gemma-3-270m-4bit"; then
    echo "   ✓ Model exists in MLX cache"
else
    echo "   ✗ Model not found in MLX cache"
fi

# Test 2: Try a simple generation with verbose output
echo ""
echo "2. Testing MLX generation with verbose output..."

# Try with a short timeout to see if we get any response
TIMEOUT=15
echo "   Starting generation with $TIMEOUT second timeout..."

# Use curl with verbose output to see what's happening
GENERATION_OUTPUT=$(curl -v http://localhost:11434/api/generate \
    -d '{"model":"mlx-community/gemma-3-270m-4bit","prompt":"Hello","stream":false}' \
    --max-time $TIMEOUT 2>&1)

GENERATION_EXIT_CODE=$?

echo "   Exit code: $GENERATION_EXIT_CODE"
echo ""
echo "   Output (first 500 chars):"
echo "$GENERATION_OUTPUT" | head -c 500
echo ""
echo "   Output (last 500 chars):"
echo "$GENERATION_OUTPUT" | tail -c 500

echo ""
echo "=== Debug Complete ==="