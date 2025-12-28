#!/usr/bin/env bash
# ollmlx Easy Installer for macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/Baswold/ollmlx/main/scripts/easy_install.sh | bash
#    or: curl -fsSL https://ollmlx.dev/install.sh | bash
#
# This script:
# 1. Detects your platform (macOS Apple Silicon only)
# 2. Downloads the latest release OR builds from source
# 3. Sets up Python environment with MLX dependencies
# 4. Installs ollmlx to your system
# 5. Verifies everything works

set -euo pipefail

# Configuration
OLLMLX_DIR="$HOME/.ollmlx"
VENV_DIR="$OLLMLX_DIR/venv"
BIN_DIR="$OLLMLX_DIR/bin"
REPO_URL="https://github.com/Baswold/ollmlx"
VERSION="${OLLMLX_VERSION:-latest}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# Logging
log() { echo -e "${BLUE}==>${NC} ${BOLD}$1${NC}"; }
success() { echo -e "${GREEN}==>${NC} ${BOLD}$1${NC}"; }
warn() { echo -e "${YELLOW}Warning:${NC} $1"; }
error() { echo -e "${RED}Error:${NC} $1" >&2; exit 1; }

# Banner
echo ""
echo -e "${BOLD}╔═══════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║          ${GREEN}ollmlx${NC}${BOLD} Easy Installer            ║${NC}"
echo -e "${BOLD}║   Run Ollama-compatible LLMs with MLX     ║${NC}"
echo -e "${BOLD}╚═══════════════════════════════════════════╝${NC}"
echo ""

# Detect platform
detect_platform() {
    local os arch
    os="$(uname -s)"
    arch="$(uname -m)"

    if [[ "$os" != "Darwin" ]]; then
        error "ollmlx currently only supports macOS. Found: $os"
    fi

    if [[ "$arch" != "arm64" ]]; then
        error "ollmlx requires Apple Silicon (M1/M2/M3/M4). Found: $arch"
    fi

    success "Detected: macOS on Apple Silicon"
}

# Check dependencies
check_dependencies() {
    log "Checking dependencies..."

    # Check for Xcode Command Line Tools
    if ! xcode-select -p &>/dev/null; then
        log "Installing Xcode Command Line Tools..."
        xcode-select --install 2>/dev/null || true
        echo ""
        echo -e "${YELLOW}Please complete the Xcode Command Line Tools installation,"
        echo -e "then run this script again.${NC}"
        exit 0
    fi
    success "Xcode Command Line Tools installed"

    # Check Python 3.10+
    if ! command -v python3 &>/dev/null; then
        error "Python 3 is required. Install with: brew install python@3.12"
    fi

    PY_VERSION=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
    PY_MAJOR=$(echo "$PY_VERSION" | cut -d. -f1)
    PY_MINOR=$(echo "$PY_VERSION" | cut -d. -f2)

    if [[ "$PY_MAJOR" -lt 3 ]] || [[ "$PY_MAJOR" -eq 3 && "$PY_MINOR" -lt 10 ]]; then
        error "Python 3.10+ required. Found: Python $PY_VERSION"
    fi
    success "Found Python $PY_VERSION"

    # Check Go (optional - only needed for building from source)
    if command -v go &>/dev/null; then
        GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
        success "Found Go $GO_VERSION"
        HAS_GO=true
    else
        HAS_GO=false
        warn "Go not found - will download pre-built binaries"
    fi
}

# Setup directories
setup_directories() {
    log "Setting up ollmlx directories..."
    mkdir -p "$OLLMLX_DIR"
    mkdir -p "$BIN_DIR"
    mkdir -p "$OLLMLX_DIR/models"
}

# Setup Python virtual environment
setup_python_env() {
    log "Setting up Python environment..."

    if [[ -d "$VENV_DIR" ]]; then
        success "Using existing virtual environment"
    else
        log "Creating virtual environment..."
        python3 -m venv "$VENV_DIR"
        success "Virtual environment created at $VENV_DIR"
    fi

    # Activate and install dependencies
    source "$VENV_DIR/bin/activate"

    log "Installing MLX dependencies (this may take a few minutes)..."
    pip install --upgrade pip -q

    # Install MLX and dependencies
    pip install -q \
        "mlx>=0.15.0" \
        "mlx-lm>=0.19.0" \
        "fastapi>=0.104.0" \
        "uvicorn>=0.24.0" \
        "pydantic>=2.0.0"

    success "Python dependencies installed"

    # Verify MLX works
    if python3 -c "import mlx.core as mx; print(mx.default_device())" &>/dev/null; then
        success "MLX is working correctly"
    else
        warn "MLX installed but may have issues"
    fi

    deactivate
}

# Download or build ollmlx
install_ollmlx() {
    log "Installing ollmlx..."

    # Try to download pre-built binaries first
    if download_release; then
        return 0
    fi

    # Fall back to building from source
    if [[ "$HAS_GO" == "true" ]]; then
        build_from_source
    else
        error "Could not download release and Go is not installed for building from source.
Install Go with: brew install go
Then run this script again."
    fi
}

# Download pre-built release
download_release() {
    log "Checking for pre-built release..."

    # Try to get latest release from GitHub
    local release_url="$REPO_URL/releases/latest/download/ollmlx-darwin-arm64.tar.gz"
    local tmp_file="/tmp/ollmlx-release.tar.gz"

    if curl -fsSL --head "$release_url" &>/dev/null; then
        log "Downloading latest release..."
        curl -fsSL "$release_url" -o "$tmp_file"

        log "Extracting..."
        tar -xzf "$tmp_file" -C "$BIN_DIR"
        rm "$tmp_file"

        # Rename to ollmlx-bin (wrapper script will be 'ollmlx')
        if [[ -f "$BIN_DIR/ollmlx" ]]; then
            mv "$BIN_DIR/ollmlx" "$BIN_DIR/ollmlx-bin"
        fi
        chmod +x "$BIN_DIR/ollmlx-bin"
        [[ -f "$BIN_DIR/ollama-runner" ]] && chmod +x "$BIN_DIR/ollama-runner"

        # Also need to get the mlx_backend
        log "Downloading MLX backend..."
        local backend_url="$REPO_URL/archive/refs/heads/main.tar.gz"
        local backend_tmp="/tmp/ollmlx-src.tar.gz"
        curl -fsSL "$backend_url" -o "$backend_tmp"
        tar -xzf "$backend_tmp" -C /tmp
        cp -r /tmp/ollmlx-main/mlx_backend "$OLLMLX_DIR/"
        rm -rf /tmp/ollmlx-main "$backend_tmp"

        success "Downloaded and installed pre-built binaries"
        return 0
    else
        warn "No pre-built release found"
        return 1
    fi
}

# Build from source
build_from_source() {
    log "Building from source..."

    local src_dir="$OLLMLX_DIR/src"

    # Clone or update repository
    if [[ -d "$src_dir/.git" ]]; then
        log "Updating source repository..."
        cd "$src_dir"
        git pull -q
    else
        log "Cloning repository..."
        rm -rf "$src_dir"
        git clone -q "$REPO_URL" "$src_dir"
        cd "$src_dir"
    fi

    # Build binaries (ollmlx-bin because wrapper will be 'ollmlx')
    log "Compiling ollmlx..."
    go build -ldflags="-s -w" -o "$BIN_DIR/ollmlx-bin" .

    log "Compiling ollama-runner..."
    go build -ldflags="-s -w" -o "$BIN_DIR/ollama-runner" ./cmd/runner

    # Copy MLX backend
    cp -r mlx_backend "$OLLMLX_DIR/"

    success "Built from source successfully"
}

# Setup shell integration
setup_shell() {
    log "Setting up shell integration..."

    local shell_config=""
    local shell_name=""

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
            warn "Unknown shell: $SHELL. Please add $BIN_DIR to your PATH manually."
            return
            ;;
    esac

    # Add to PATH if not already there
    local path_line="export PATH=\"$BIN_DIR:\$PATH\""

    if ! grep -q "$BIN_DIR" "$shell_config" 2>/dev/null; then
        echo "" >> "$shell_config"
        echo "# ollmlx" >> "$shell_config"
        echo "$path_line" >> "$shell_config"
        success "Added ollmlx to PATH in $shell_config"
    else
        success "PATH already configured"
    fi

    # Export for current session
    export PATH="$BIN_DIR:$PATH"
}

# Create wrapper that makes 'ollmlx' just work
create_wrapper() {
    log "Creating ollmlx command wrapper..."

    # The actual binary goes to ollmlx-bin
    if [[ -f "$BIN_DIR/ollmlx" ]]; then
        mv "$BIN_DIR/ollmlx" "$BIN_DIR/ollmlx-bin"
    fi

    # Create the 'ollmlx' wrapper that sets up environment automatically
    cat > "$BIN_DIR/ollmlx" << 'EOF'
#!/usr/bin/env bash
# ollmlx wrapper - automatically sets up Python environment
export OLLMLX_HOME="$HOME/.ollmlx"
export OLLAMA_PYTHON="$OLLMLX_HOME/venv/bin/python3"
export OLLMLX_BACKEND="$OLLMLX_HOME/mlx_backend"
exec "$OLLMLX_HOME/bin/ollmlx-bin" "$@"
EOF
    chmod +x "$BIN_DIR/ollmlx"

    success "Created ollmlx command"
}

# Verify installation
verify_installation() {
    log "Verifying installation..."

    # The wrapper script handles environment setup automatically
    export PATH="$BIN_DIR:$PATH"

    if "$BIN_DIR/ollmlx" doctor 2>/dev/null; then
        success "Installation verified!"
    else
        warn "Some checks failed, but ollmlx may still work"
    fi
}

# Print success message
print_success() {
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║     ${BOLD}ollmlx installed successfully!${NC}${GREEN}        ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BOLD}Quick Start:${NC}"
    echo ""
    echo -e "  ${BLUE}1.${NC} Start a new terminal (or run: source ~/.zshrc)"
    echo ""
    echo -e "  ${BLUE}2.${NC} Start the server:"
    echo -e "     ${GREEN}ollmlx serve${NC}"
    echo ""
    echo -e "  ${BLUE}3.${NC} In another terminal, pull and run a model:"
    echo -e "     ${GREEN}ollmlx pull mlx-community/gemma-3-270m-4bit${NC}"
    echo -e "     ${GREEN}ollmlx run mlx-community/gemma-3-270m-4bit${NC}"
    echo ""
    echo -e "${BOLD}Useful Commands:${NC}"
    echo -e "  ollmlx list      - List installed models"
    echo -e "  ollmlx doctor    - Check system status"
    echo -e "  ollmlx --help    - Show all commands"
    echo ""
    echo -e "${BOLD}Documentation:${NC} $REPO_URL"
    echo ""
}

# Uninstall function (for reference)
uninstall() {
    echo "To uninstall ollmlx, run:"
    echo "  rm -rf ~/.ollmlx"
    echo "  # Remove the PATH line from ~/.zshrc or ~/.bashrc"
}

# Main installation flow
main() {
    detect_platform
    check_dependencies
    setup_directories
    setup_python_env
    install_ollmlx
    create_wrapper
    setup_shell
    verify_installation
    print_success
}

# Handle arguments
case "${1:-}" in
    --uninstall)
        uninstall
        ;;
    --help|-h)
        echo "ollmlx Easy Installer"
        echo ""
        echo "Usage: $0 [OPTIONS]"
        echo ""
        echo "Options:"
        echo "  --help       Show this help message"
        echo "  --uninstall  Show uninstall instructions"
        echo ""
        echo "Environment Variables:"
        echo "  OLLMLX_VERSION  Specify version to install (default: latest)"
        ;;
    *)
        main
        ;;
esac
