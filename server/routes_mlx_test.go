package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/envconfig"
)

func TestStartMLXRunnerPropagatesModelsEnv(t *testing.T) {
	t.Setenv("OLLAMA_MODELS", t.TempDir())

	cmd, _, err := startMLXRunner(context.Background(), "test-model")
	if err != nil {
		t.Fatalf("startMLXRunner() error = %v", err)
	}

	expected := fmt.Sprintf("OLLAMA_MODELS=%s", envconfig.Models())
	found := false
	for _, env := range cmd.Env {
		if env == expected {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected runner environment to include %q", expected)
	}
}

func TestGenerateMLXModelUsesLocalName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	modelName := "mlx-community/llama-2"
	localName := strings.ReplaceAll(modelName, "/", "_")

	modelsRoot := t.TempDir()
	t.Setenv("OLLAMA_MODELS", modelsRoot)

	modelDir := filepath.Join(modelsRoot, "mlx", localName)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatalf("failed to create model directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(modelDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "weights.npz"), []byte{}, 0o644); err != nil {
		t.Fatalf("failed to write weights: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/completion", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintf(w, `{"content":"ok","done":true,"done_reason":"stop"}\n`)
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})

	port := listener.Addr().(*net.TCPAddr).Port

	var startedModel string
	startMLXRunnerFunc = func(ctx context.Context, modelName string) (*exec.Cmd, int, error) {
		startedModel = modelName
		return exec.CommandContext(ctx, "true"), port, nil
	}
	defer func() { startMLXRunnerFunc = startMLXRunner }()

	var loadedModel string
	loadMLXModelFunc = func(_ context.Context, _ *http.Client, p int, modelName string) error {
		if p != port {
			t.Fatalf("unexpected port: got %d want %d", p, port)
		}
		loadedModel = modelName
		return nil
	}
	defer func() { loadMLXModelFunc = loadMLXModel }()

	stream := false
	req := &api.GenerateRequest{Model: modelName, Prompt: "Hello", Stream: &stream}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/generate", nil)

	srvInstance := &Server{}
	srvInstance.generateMLXModel(c, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d body %s", w.Code, w.Body.String())
	}

	if startedModel != localName {
		t.Fatalf("runner received %q, want %q", startedModel, localName)
	}

	if loadedModel != localName {
		t.Fatalf("loader received %q, want %q", loadedModel, localName)
	}
}

func TestWaitForMLXRunnerPropagatesHealthError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "backend unhealthy", http.StatusServiceUnavailable)
		}),
	}

	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	port := listener.Addr().(*net.TCPAddr).Port

	err = waitForMLXRunner(ctx, client, port)
	if err == nil {
		t.Fatalf("expected waitForMLXRunner to fail")
	}

	if !strings.Contains(err.Error(), "backend unhealthy") {
		t.Fatalf("expected error message to include backend response, got: %v", err)
	}
}
