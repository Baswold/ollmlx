# ollmlx Makefile
# Production-ready build and test commands

.PHONY: all build build-app clean test install dmg serve doctor help

# Default target
all: build

# Build the main binary
build:
	@echo "Building ollmlx..."
	go build -o ollmlx .
	@echo "Done: ./ollmlx"

# Build with optimizations
build-release:
	@echo "Building ollmlx (release)..."
	go build -ldflags="-s -w" -o ollmlx .
	@echo "Done: ./ollmlx"

# Build the macOS app (macOS only)
build-app:
	@echo "Building macOS app..."
	./scripts/build_app.sh

# Create DMG installer (macOS only)
dmg: build-app
	@echo "Creating DMG installer..."
	./scripts/package_dmg.sh

# Run tests
test:
	@echo "Running tests..."
	go test ./... -short -v

# Run MLX backend tests
test-mlx:
	@echo "Running MLX backend tests..."
	cd mlx_backend && python -m pytest test_server.py -v

# Run integration test with Gemma
test-gemma:
	@echo "Running Gemma MLX test..."
	./scripts/test_gemma_mlx.sh

# Install (builds and installs to /usr/local/bin)
install:
	@echo "Installing ollmlx..."
	./scripts/install_ollmlx.sh --install

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf ollmlx ollama-runner dist/
	go clean

# Start the server
serve: build
	./ollmlx serve

# Run diagnostics
doctor: build
	./ollmlx doctor

# Setup development environment
setup:
	@echo "Setting up development environment..."
	go mod download
	@if [ -d "mlx_backend" ]; then \
		python3 -m venv ~/.ollmlx/venv; \
		~/.ollmlx/venv/bin/pip install -r mlx_backend/requirements.txt; \
	fi
	@echo "Done!"

# Format code
fmt:
	go fmt ./...
	@if command -v black >/dev/null 2>&1; then \
		black mlx_backend/*.py; \
	fi

# Lint code
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		go vet ./...; \
	fi

# Show help
help:
	@echo "ollmlx Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build        Build the ollmlx binary"
	@echo "  build-release Build with optimizations"
	@echo "  build-app    Build the macOS menu bar app"
	@echo "  dmg          Create DMG installer (macOS)"
	@echo "  test         Run Go tests"
	@echo "  test-mlx     Run MLX backend tests"
	@echo "  test-gemma   Run Gemma integration test"
	@echo "  install      Build and install to /usr/local/bin"
	@echo "  clean        Remove build artifacts"
	@echo "  serve        Build and start server"
	@echo "  doctor       Run diagnostics"
	@echo "  setup        Setup development environment"
	@echo "  fmt          Format code"
	@echo "  lint         Lint code"
	@echo "  help         Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build && ./ollmlx serve"
	@echo "  make test-gemma"
	@echo "  make dmg"
