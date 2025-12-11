#!/bin/bash

echo "=== Testing MLX Generation ==="
echo ""

# Test 1: Check if server is running
echo "1. Checking server status..."
if curl -s http://localhost:11434/api/version > /dev/null 2>&1; then
    echo "   ✓ Server is running"
else
    echo "   ✗ Server not running"
    exit 1
fi

# Test 2: List MLX models
echo "2. Listing MLX models..."
./ollmlx list | grep mlx || echo "   No MLX models found"

# Test 3: Try MLX generation with timeout
echo "3. Testing MLX generation..."
echo "   Attempting to generate with mlx-community/gemma-3-270m-4bit"

# Use a background process with timeout
(
    sleep 30 && 
    echo "   ✗ Generation timed out after 30 seconds" && 
    exit 1
) &
TIMEOUT_PID=$!

# Start generation in background
GENERATION_OUTPUT=$(curl -s http://localhost:11434/api/generate -d '{"model":"mlx-community/gemma-3-270m-4bit","prompt":"Why is the sky blue?","stream":false,"options":{"temperature":0.7}}')
GENERATION_EXIT_CODE=$?

# Kill timeout process
kill $TIMEOUT_PID 2>/dev/null
wait $TIMEOUT_PID 2>/dev/null

if [ $GENERATION_EXIT_CODE -eq 0 ]; then
    echo "   ✓ Generation completed"
    echo "   Response: $GENERATION_OUTPUT" | head -c 200
else
    echo "   ✗ Generation failed with exit code $GENERATION_EXIT_CODE"
fi

echo ""
echo "=== Test Complete ==="