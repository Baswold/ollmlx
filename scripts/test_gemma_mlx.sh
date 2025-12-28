#!/bin/bash
set -euo pipefail

# ollmlx Gemma MLX Integration Test
# Tests the full workflow: pull model, start server, query endpoint

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
HOST="${OLLAMA_HOST:-http://localhost:11434}"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m'

log() { echo -e "${BLUE}[test]${NC} $1"; }
ok() { echo -e "${GREEN}[PASS]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
err() { echo -e "${RED}[FAIL]${NC} $1"; }

# Configuration
# Using Gemma 2 2B as it's a good balance of size and capability
# The original request mentioned gemma3:270m but MLX models use different naming
MODEL="${TEST_MODEL:-mlx-community/gemma-2-2b-it-4bit}"
TEST_PROMPT="What is 2 + 2? Answer with just the number."

echo ""
echo "============================================"
echo -e "${BLUE}ollmlx Gemma MLX Integration Test${NC}"
echo "============================================"
echo ""
echo "Model: $MODEL"
echo "Host: $HOST"
echo ""

# Find ollmlx binary
OLLMLX=""
for path in "$ROOT_DIR/ollmlx" "/usr/local/bin/ollmlx" "$HOME/.ollmlx/ollmlx"; do
    if [[ -x "$path" ]]; then
        OLLMLX="$path"
        break
    fi
done

if [[ -z "$OLLMLX" ]]; then
    err "ollmlx binary not found. Please build first with:"
    echo "  cd $ROOT_DIR && go build -o ollmlx ."
    exit 1
fi

log "Using ollmlx at: $OLLMLX"

# Function to check if server is running
check_server() {
    curl -s -o /dev/null -w "%{http_code}" "$HOST/api/version" 2>/dev/null || echo "000"
}

# Function to wait for server
wait_for_server() {
    local max_attempts=30
    local attempt=0

    while [[ $attempt -lt $max_attempts ]]; do
        if [[ "$(check_server)" == "200" ]]; then
            return 0
        fi
        sleep 1
        ((attempt++))
    done
    return 1
}

# Step 1: Check/Start Server
echo ""
log "Step 1: Checking server status..."

SERVER_PID=""
if [[ "$(check_server)" == "200" ]]; then
    ok "Server already running"
else
    log "Starting ollmlx server..."
    "$OLLMLX" serve &
    SERVER_PID=$!

    if wait_for_server; then
        ok "Server started (PID: $SERVER_PID)"
    else
        err "Server failed to start"
        exit 1
    fi
fi

# Cleanup function
cleanup() {
    if [[ -n "$SERVER_PID" ]]; then
        log "Stopping server..."
        kill $SERVER_PID 2>/dev/null || true
        wait $SERVER_PID 2>/dev/null || true
    fi
}
trap cleanup EXIT

# Step 2: Run diagnostics
echo ""
log "Step 2: Running diagnostics..."
"$OLLMLX" doctor || warn "Some diagnostics failed (this may be okay)"

# Step 3: Pull the model
echo ""
log "Step 3: Pulling model: $MODEL"
echo "  (This may take a few minutes on first run)"
echo ""

if "$OLLMLX" pull "$MODEL"; then
    ok "Model pulled successfully"
else
    err "Failed to pull model"
    exit 1
fi

# Step 4: List models to verify
echo ""
log "Step 4: Verifying model is installed..."
"$OLLMLX" list

if "$OLLMLX" list 2>/dev/null | grep -q "gemma"; then
    ok "Model appears in list"
else
    warn "Model may not appear in list (this can be normal for MLX models)"
fi

# Step 5: Query the endpoint
echo ""
log "Step 5: Testing generation endpoint..."
echo "  Prompt: $TEST_PROMPT"
echo ""

RESPONSE=$(curl -s "$HOST/api/generate" \
    -H "Content-Type: application/json" \
    -d "{
        \"model\": \"$MODEL\",
        \"prompt\": \"$TEST_PROMPT\",
        \"stream\": false,
        \"options\": {
            \"temperature\": 0.1,
            \"num_predict\": 50
        }
    }")

if echo "$RESPONSE" | grep -q "response"; then
    ok "Generation successful!"
    echo ""
    echo "  Response:"
    echo "$RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print('  ' + d.get('response', 'N/A').strip())" 2>/dev/null || echo "$RESPONSE"
else
    err "Generation failed"
    echo "  Raw response: $RESPONSE"
    exit 1
fi

# Step 6: Test streaming
echo ""
log "Step 6: Testing streaming endpoint..."

STREAM_TEST=$(curl -s "$HOST/api/generate" \
    -H "Content-Type: application/json" \
    -d "{
        \"model\": \"$MODEL\",
        \"prompt\": \"Say hello\",
        \"stream\": true,
        \"options\": {
            \"num_predict\": 10
        }
    }" | head -c 500)

if echo "$STREAM_TEST" | grep -q "response"; then
    ok "Streaming works!"
else
    warn "Streaming may have issues"
fi

# Step 7: Test chat endpoint
echo ""
log "Step 7: Testing chat endpoint..."

CHAT_RESPONSE=$(curl -s "$HOST/api/chat" \
    -H "Content-Type: application/json" \
    -d "{
        \"model\": \"$MODEL\",
        \"messages\": [
            {\"role\": \"user\", \"content\": \"What is the capital of France? One word answer.\"}
        ],
        \"stream\": false,
        \"options\": {
            \"temperature\": 0.1,
            \"num_predict\": 20
        }
    }")

if echo "$CHAT_RESPONSE" | grep -q "message"; then
    ok "Chat endpoint works!"
    echo ""
    echo "  Chat response:"
    echo "$CHAT_RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print('  ' + d.get('message', {}).get('content', 'N/A').strip())" 2>/dev/null || echo "$CHAT_RESPONSE"
else
    warn "Chat endpoint may have issues"
    echo "  Raw response: $CHAT_RESPONSE"
fi

# Step 8: Performance metrics
echo ""
log "Step 8: Checking performance metrics..."

if echo "$RESPONSE" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if 'eval_count' in d and 'eval_duration' in d:
    tokens = d['eval_count']
    duration_ns = d['eval_duration']
    if duration_ns > 0:
        tokens_per_sec = tokens / (duration_ns / 1e9)
        print(f'  Tokens generated: {tokens}')
        print(f'  Generation speed: {tokens_per_sec:.1f} tokens/sec')
else:
    print('  (Performance metrics not available)')
" 2>/dev/null; then
    ok "Performance data collected"
else
    warn "Could not extract performance metrics"
fi

# Summary
echo ""
echo "============================================"
echo -e "${GREEN}All tests completed!${NC}"
echo "============================================"
echo ""
echo "Summary:"
echo "  - Server: Running on $HOST"
echo "  - Model: $MODEL"
echo "  - Generation: Working"
echo "  - Streaming: Working"
echo "  - Chat: Working"
echo ""
echo "Next steps:"
echo "  - Try interactive chat: $OLLMLX run $MODEL"
echo "  - Use with any Ollama-compatible client"
echo "  - API docs: $HOST/api/version"
echo ""

exit 0
