package mlxrunner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMLXBackendPathFromSubdir(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})

	repoRoot := findProjectRoot(origWD)
	if repoRoot == "" {
		t.Fatal("unable to find repository root from working directory")
	}

	subdir := filepath.Join(repoRoot, "runner")
	if err := os.Chdir(subdir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	backendPath, err := findMLXBackendPath()
	if err != nil {
		t.Fatalf("failed to find backend path: %v", err)
	}

	expected := filepath.Join(repoRoot, "mlx_backend", "server.py")
	if backendPath != expected {
		t.Fatalf("expected backend path %s, got %s", expected, backendPath)
	}
}
