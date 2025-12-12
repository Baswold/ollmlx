package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompiledMLXRunnerLaunches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MLX runner binary test in short mode")
	}

	tmpDir := t.TempDir()
	runnerBin := filepath.Join(tmpDir, "ollama-runner")
	if runtime.GOOS == "windows" {
		runnerBin += ".exe"
	}

	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("failed to resolve project root: %v", err)
	}

	build := exec.Command("go", "build", "-o", runnerBin, "./cmd/runner")
	build.Dir = projectRoot
	build.Env = os.Environ()
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build runner binary: %v\n%s", err, string(output))
	}

	cmd := exec.Command(runnerBin, "--mlx-engine", "-h")
	helpOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to launch compiled runner: %v\n%s", err, string(helpOut))
	}

	if !strings.Contains(strings.ToLower(string(helpOut)), "mlx runner") {
		t.Fatalf("expected runner help output to mention mlxrunner, got: %s", string(helpOut))
	}
}
