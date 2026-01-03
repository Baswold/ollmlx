#!/usr/bin/env bash
set -euo pipefail

# ollmlx Installer
# 1. Checks prerequisites (Go, Python 3.10+)
# 2. Sets up a dedicated Python virtual environment (~/.ollmlx/venv)
# 3. Installs MLX dependencies
# 4. Builds the Go binary
# 5. Optional: Installs to system path

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OLLMLX_DIR="$HOME/.ollmlx"
VENV_DIR="$OLLMLX_DIR/venv"

# --- Colors ---
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

log() { echo -e "${BLUE}[ollmlx]${NC} $1"; }
ok() { echo -e "${GREEN}[ok]${NC} $1"; }
warn() { echo -e "${RED}[warn]${NC} $1"; }
err() { echo -e "${RED}[error]${NC} $1" >&2; exit 1; }

# --- 1. Prerequisites ---
log "Checking prerequisites..."

# Check Go
if ! command -v go >/dev/null; then
  err "Go is required but not found. Please install Go 1.21+"
fi
# Simple go version check (heuristic)
GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
ok "Found Go $GO_VER"

# Check Python 3.10+
if ! command -v python3 >/dev/null; then
  err "Python 3 is required but not found."
fi

PY_CHECK=$(python3 -c "import sys; print(1) if sys.version_info >= (3, 10) else print(0)")
if [ "$PY_CHECK" != "1" ]; then
  err "Python 3.10 or higher is required."
fi
ok "Found Python $(python3 --version)"

# --- 2. Virtual Environment ---
log "Setting up Python environment..."
if [ ! -d "$VENV_DIR" ]; then
  log "Creating virtual environment at $VENV_DIR"
  mkdir -p "$OLLMLX_DIR"
  python3 -m venv "$VENV_DIR"
else
  ok "Using existing virtual environment at $VENV_DIR"
fi

# Activate venv for dependency installation
source "$VENV_DIR/bin/activate"

# --- 3. Dependencies ---
REQ_FILE="$ROOT/mlx_backend/requirements.txt"
if [ -f "$REQ_FILE" ]; then
  log "Installing/Updating Python dependencies..."
  pip install --upgrade pip -q
  pip install -r "$REQ_FILE" -q
  ok "Python dependencies installed"
else
  err "requirements.txt not found at $REQ_FILE"
fi

# --- 4. Build ---
log "Building ollmlx binary..."
cd "$ROOT"
go build -o ollmlx .
ok "Build complete: ./ollmlx"

log "Building ollama-runner binary..."
go build -o ollama-runner ./cmd/runner
ok "Build complete: ./ollama-runner"

# --- 5. Installation ---
BIN_DIR="$OLLMLX_DIR/bin"
mkdir -p "$BIN_DIR"

log "Installing binaries to $BIN_DIR..."
cp ollmlx "$BIN_DIR/ollmlx-bin"
cp ollama-runner "$BIN_DIR/ollama-runner"
chmod +x "$BIN_DIR/ollmlx-bin" "$BIN_DIR/ollama-runner"

# Create wrapper script
log "Creating ollmlx command wrapper..."
cat > "$BIN_DIR/ollmlx" << 'WRAPPER_EOF'
#!/usr/bin/env bash
# ollmlx wrapper - automatically sets up Python environment
export OLLMLX_HOME="$HOME/.ollmlx"
export OLLAMA_PYTHON="$OLLMLX_HOME/venv/bin/python3"
export OLLMLX_BACKEND="$OLLMLX_HOME/mlx_backend"
exec "$OLLMLX_HOME/bin/ollmlx-bin" "$@"
WRAPPER_EOF
chmod +x "$BIN_DIR/ollmlx"

# Copy mlx_backend to .ollmlx directory
log "Installing MLX backend..."
cp -r "$ROOT/mlx_backend" "$OLLMLX_DIR/"

ok "Binaries installed to $BIN_DIR"

# --- 6. Shell Integration ---
log "Setting up shell integration..."

shell_config=""
shell_name=""

# Detect shell
case "$SHELL" in
  */zsh)
    shell_config="$HOME/.zshrc"
    shell_name="zsh"
    ;;
  */bash)
    if [[ -f "$HOME/.bash_profile" ]]; then
      shell_config="$HOME/.bash_profile"
    else
      shell_config="$HOME/.bashrc"
    fi
    shell_name="bash"
    ;;
  *)
    warn "Unknown shell: $SHELL"
    shell_config=""
    ;;
esac

# Add to PATH if not already there
if [[ -n "$shell_config" ]]; then
  path_line="export PATH=\"$BIN_DIR:\$PATH\""

  if ! grep -q "$BIN_DIR" "$shell_config" 2>/dev/null; then
    echo "" >> "$shell_config"
    echo "# ollmlx" >> "$shell_config"
    echo "$path_line" >> "$shell_config"
    ok "Added ollmlx to PATH in $shell_config"
  else
    ok "PATH already configured in $shell_config"
  fi
fi

echo ""
echo "--------------------------------------------------"
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo -e "Binaries installed to: ${GREEN}$BIN_DIR${NC}"
echo -e "MLX backend at:        ${GREEN}$OLLMLX_DIR/mlx_backend${NC}"
echo -e "Python venv at:        ${GREEN}$VENV_DIR${NC}"
echo ""
echo -e "${BLUE}To start using ollmlx immediately:${NC}"
echo -e "  ${GREEN}export PATH=\"$BIN_DIR:\$PATH\"${NC}"
echo ""
echo -e "${BLUE}Or restart your terminal, then run:${NC}"
echo -e "  ${GREEN}ollmlx doctor${NC}   # Verify installation"
echo -e "  ${GREEN}ollmlx serve${NC}    # Start the server"
echo ""
if [[ $# -gt 0 && "$1" == "--install" ]]; then
  DEST_DIR="/usr/local/bin"
  log "Also installing to system-wide location $DEST_DIR (may require password)"
  sudo cp "$BIN_DIR/ollmlx" "$DEST_DIR/ollmlx"
  sudo cp "$BIN_DIR/ollama-runner" "$DEST_DIR/ollama-runner"
  ok "System-wide installation complete"
fi
echo "--------------------------------------------------"

