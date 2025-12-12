package mlxrunner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/logutil"
)

// Server manages the MLX Python backend subprocess and proxies requests to it
type Server struct {
	modelPath  string
	mlxPort    int
	mlxCmd     *exec.Cmd
	mlxClient  *http.Client
	status     llm.ServerStatus
	ready      sync.WaitGroup
	mu         sync.Mutex
	cond       *sync.Cond
	pythonPath string
}

// LoadRequest matches the structure expected by the MLX backend
type LoadRequest struct {
	ModelPath string `json:"model_path"`
}

// LoadResponse from MLX backend
type LoadResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// findProjectRoot walks upward from a starting directory to find the repository
// root, identified by the presence of a go.mod file.
func findProjectRoot(start string) string {
	for dir := filepath.Clean(start); dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return ""
}

// findMLXBackendPath derives potential locations for the MLX backend server
// script based on the executable path and repository root, logs each candidate,
// and returns the first existing path.
func findMLXBackendPath() (string, error) {
	var candidates []string

	exePath, exeErr := os.Executable()
	if exeErr == nil {
		exeDir := filepath.Dir(exePath)
		exeRoot := findProjectRoot(exeDir)
		for _, root := range []string{exeDir, exeRoot} {
			if root == "" {
				continue
			}
			candidate := filepath.Join(root, "mlx_backend", "server.py")
			candidates = append(candidates, candidate)
		}
	} else {
		slog.Warn("unable to resolve executable path", "error", exeErr)
	}

	if wd, err := os.Getwd(); err == nil {
		if projectRoot := findProjectRoot(wd); projectRoot != "" {
			candidates = append(candidates, filepath.Join(projectRoot, "mlx_backend", "server.py"))
		}
	}

	seen := make(map[string]struct{})
	var uniqueCandidates []string
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		uniqueCandidates = append(uniqueCandidates, candidate)
	}

	for _, candidate := range uniqueCandidates {
		slog.Info("checking MLX backend candidate", "path", candidate)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	if exeErr != nil {
		exePath = "<unknown>"
	}
	wd, _ := os.Getwd()
	return "", fmt.Errorf("MLX backend server not found; candidates: %s; executable: %s; working directory: %s", strings.Join(uniqueCandidates, ", "), exePath, wd)
}

// startMLXBackend launches the Python MLX backend server
func (s *Server) startMLXBackend(ctx context.Context) error {
	// Find Python executable
	pythonExe := "python3"
	if s.pythonPath != "" {
		pythonExe = s.pythonPath
	}

	// Locate the MLX backend server script
	mlxBackendPath, err := findMLXBackendPath()
	if err != nil {
		return err
	}

	// Allocate a random port for the MLX backend
	s.mlxPort = 0
	if a, err := net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			s.mlxPort = l.Addr().(*net.TCPAddr).Port
			l.Close()
		}
	}
	if s.mlxPort == 0 {
		s.mlxPort = 9090 // fallback port
	}

	slog.Info("starting MLX backend", "port", s.mlxPort, "path", mlxBackendPath)

	// Start the MLX backend Python server
	s.mlxCmd = exec.CommandContext(ctx, pythonExe, mlxBackendPath, "--port", strconv.Itoa(s.mlxPort))
	s.mlxCmd.Env = os.Environ()

	// Capture stdout/stderr for debugging
	stdout, _ := s.mlxCmd.StdoutPipe()
	stderr, _ := s.mlxCmd.StderrPipe()

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			slog.Debug("mlx backend stdout", "line", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			slog.Warn("mlx backend stderr", "line", scanner.Text())
		}
	}()

	if err := s.mlxCmd.Start(); err != nil {
		return fmt.Errorf("failed to start MLX backend: %w", err)
	}

	// Wait for the MLX backend to be ready
	mlxURL := fmt.Sprintf("http://127.0.0.1:%d/health", s.mlxPort)
	for i := 0; i < 30; i++ {
		resp, err := s.mlxClient.Get(mlxURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			slog.Info("MLX backend is ready")
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("MLX backend failed to start within timeout")
}

// load handles the /load endpoint - loads the model into MLX backend
func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The model should already be set from command line, but we can accept it here too
	var req llm.LoadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	slog.Info("loading model into MLX backend", "model", s.modelPath)

	// Send load request to MLX backend using model name
	loadReq := map[string]string{"model": s.modelPath}
	reqBody, err := json.Marshal(loadReq)
	if err != nil {
		slog.Error("failed to marshal MLX load request", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mlxURL := fmt.Sprintf("http://127.0.0.1:%d/load", s.mlxPort)
	resp, err := s.mlxClient.Post(mlxURL, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		slog.Error("failed to load model in MLX backend", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read MLX backend load response", "error", err)
			http.Error(w, "failed to read backend response", http.StatusInternalServerError)
			return
		}

		slog.Error("MLX backend load failed", "status", resp.StatusCode, "body", string(body))
		http.Error(w, string(body), resp.StatusCode)
		return
	}

	s.mu.Lock()
	s.status = llm.ServerStatusReady
	s.mu.Unlock()
	s.ready.Done()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(llm.LoadResponse{})
}

// completion handles the /completion endpoint - proxies to MLX backend
func (s *Server) completion(w http.ResponseWriter, r *http.Request) {
	s.ready.Wait()

	// Read the completion request
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Forward to MLX backend
	mlxURL := fmt.Sprintf("http://127.0.0.1:%d/completion", s.mlxPort)
	resp, err := s.mlxClient.Post(mlxURL, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to forward completion to MLX backend", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Stream the response back
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(resp.StatusCode)

	// Copy the streaming response
	if _, err := io.Copy(w, resp.Body); err != nil {
		slog.Error("error streaming response", "error", err)
	}
}

// embeddings handles the /embedding endpoint
func (s *Server) embeddings(w http.ResponseWriter, r *http.Request) {
	s.ready.Wait()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Forward to MLX backend
	mlxURL := fmt.Sprintf("http://127.0.0.1:%d/embedding", s.mlxPort)
	resp, err := s.mlxClient.Post(mlxURL, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to get embeddings from MLX backend", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// health handles the /health endpoint
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// Check both our health and MLX backend health
	mlxURL := fmt.Sprintf("http://127.0.0.1:%d/health", s.mlxPort)
	resp, err := s.mlxClient.Get(mlxURL)
	if err != nil {
		http.Error(w, "MLX backend unhealthy", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "MLX backend unhealthy", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// Execute starts the MLX runner server
func Execute(args []string) error {
	fs := flag.NewFlagSet("mlxrunner", flag.ExitOnError)
	mpath := fs.String("model", "", "Path to model or model name")
	port := fs.Int("port", 8080, "Port to expose the server on")
	pythonPath := fs.String("python", "python3", "Path to Python executable")
	_ = fs.Bool("verbose", false, "verbose output (default: disabled)")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "MLX Runner usage\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	slog.SetDefault(logutil.NewLogger(os.Stderr, envconfig.LogLevel()))
	slog.Info("starting MLX runner")

	server := &Server{
		modelPath:  *mpath,
		status:     llm.ServerStatusLaunched,
		mlxClient:  &http.Client{Timeout: 5 * time.Minute},
		pythonPath: *pythonPath,
	}

	server.ready.Add(1)
	server.cond = sync.NewCond(&server.mu)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the MLX backend
	if err := server.startMLXBackend(ctx); err != nil {
		return fmt.Errorf("failed to start MLX backend: %w", err)
	}
	defer func() {
		if server.mlxCmd != nil && server.mlxCmd.Process != nil {
			server.mlxCmd.Process.Kill()
		}
	}()

	// Start the HTTP server
	addr := "127.0.0.1:" + strconv.Itoa(*port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("Listen error:", err)
		return err
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /load", server.load)
	mux.HandleFunc("/embedding", server.embeddings)
	mux.HandleFunc("/completion", server.completion)
	mux.HandleFunc("/health", server.health)

	httpServer := http.Server{
		Handler: mux,
	}

	log.Println("MLX Runner server listening on", addr)
	if err := httpServer.Serve(listener); err != nil {
		slog.Error("server error", "error", err)
		return err
	}

	return nil
}
