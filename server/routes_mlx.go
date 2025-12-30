package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/envconfig"
	"github.com/ollama/ollama/llm"
	"github.com/ollama/ollama/ml"
)

// PullHuggingFaceModel downloads a model from HuggingFace (MLX or other formats)
func PullHuggingFaceModel(ctx context.Context, modelName string, fn func(api.ProgressResponse)) error {
	slog.Info("pulling model from HuggingFace", "model", modelName)

	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(modelName)))

	manager := llm.NewMLXModelManager()

	// Check if model already exists
	if manager.ModelExists(modelName) {
		fn(api.ProgressResponse{
			Status: fmt.Sprintf("model %s already exists", modelName),
		})
		return nil
	}

	// Download the model
	fn(api.ProgressResponse{
		Status: fmt.Sprintf("pulling %s from HuggingFace", modelName),
	})

	err := manager.DownloadMLXModel(ctx, modelName, func(status string, completed int64, total int64) {
		fn(api.ProgressResponse{
			Status:    status,
			Digest:    digest,
			Completed: completed,
			Total:     total,
		})
	})

	if err != nil {
		return fmt.Errorf("failed to download MLX model: %w", err)
	}

	fn(api.ProgressResponse{
		Status: "success",
	})

	return nil
}

type mlxCompletionChunk struct {
	Content            string         `json:"content"`
	Done               bool           `json:"done"`
	DoneReason         string         `json:"done_reason"`
	PromptEvalCount    int            `json:"prompt_eval_count"`
	PromptEvalDuration time.Duration  `json:"prompt_eval_duration"`
	EvalCount          int            `json:"eval_count"`
	EvalDuration       time.Duration  `json:"eval_duration"`
	Logprobs           any            `json:"logprobs"`
	ToolCalls          []api.ToolCall `json:"tool_calls"`
}

var (
	startMLXRunnerFunc = startMLXRunner
	loadMLXModelFunc   = loadMLXModel
)

type mlxRunnerEntry struct {
	model     string
	port      int
	cmd       *exec.Cmd
	client    *http.Client
	cancel    context.CancelFunc
	ready     chan struct{}
	err       error
	lastUsed  time.Time
	keepalive time.Duration
}

type mlxRunnerCache struct {
	mu       sync.Mutex
	entries  map[string]*mlxRunnerEntry
	ticker   *time.Ticker
	shutdown chan struct{}
}

func newMLXRunnerCache() *mlxRunnerCache {
	c := &mlxRunnerCache{
		entries:  make(map[string]*mlxRunnerEntry),
		ticker:   time.NewTicker(30 * time.Second),
		shutdown: make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-c.ticker.C:
				c.evictExpired()
			case <-c.shutdown:
				c.ticker.Stop()
				return
			}
		}
	}()

	return c
}

func (c *mlxRunnerCache) touch(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[model]; ok {
		entry.lastUsed = time.Now()
	}
}

func (c *mlxRunnerCache) evict(model string) {
	c.mu.Lock()
	entry, ok := c.entries[model]
	if ok {
		delete(c.entries, model)
	}
	c.mu.Unlock()
	if ok {
		c.stopEntry(entry)
	}
}

func (c *mlxRunnerCache) evictExpired() {
	now := time.Now()
	var stale []*mlxRunnerEntry

	c.mu.Lock()
	for k, v := range c.entries {
		if v.keepalive > 0 && now.Sub(v.lastUsed) > v.keepalive {
			stale = append(stale, v)
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()

	for _, v := range stale {
		c.stopEntry(v)
	}
}

func (c *mlxRunnerCache) stopEntry(entry *mlxRunnerEntry) {
	if entry == nil {
		return
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	if entry.cmd != nil && entry.cmd.Process != nil {
		_ = entry.cmd.Process.Kill()
	}
}

func (c *mlxRunnerCache) getRunner(ctx context.Context, model string, keepalive time.Duration) (*mlxRunnerEntry, error) {
	if keepalive < 0 {
		keepalive = 0
	}

	c.mu.Lock()
	if existing, ok := c.entries[model]; ok {
		if keepalive > existing.keepalive {
			existing.keepalive = keepalive
		}
		existing.lastUsed = time.Now()
		entry := existing
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
			if entry.err != nil {
				return nil, entry.err
			}
			return entry, nil
		}
	}

	entry := &mlxRunnerEntry{
		model:     model,
		keepalive: keepalive,
		ready:     make(chan struct{}),
		lastUsed:  time.Now(),
		client:    &http.Client{Timeout: 30 * time.Minute},
	}
	c.entries[model] = entry
	c.mu.Unlock()

	go func() {
		bgCtx, cancel := context.WithCancel(context.Background())
		entry.cancel = cancel

		cmd, port, err := startMLXRunnerFunc(bgCtx, model)
		if err != nil {
			entry.err = err
			close(entry.ready)
			return
		}
		entry.cmd = cmd
		entry.port = port

		if err := entry.cmd.Start(); err != nil {
			entry.err = err
			close(entry.ready)
			return
		}

		if err := waitForMLXRunner(bgCtx, entry.client, port); err != nil {
			entry.err = err
			_ = entry.cmd.Process.Kill()
			close(entry.ready)
			return
		}

		if err := loadMLXModelFunc(bgCtx, entry.client, port, model); err != nil {
			entry.err = err
			_ = entry.cmd.Process.Kill()
			close(entry.ready)
			return
		}

		close(entry.ready)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-entry.ready:
		if entry.err != nil {
			c.evict(model)
			return nil, entry.err
		}
		return entry, nil
	}
}

var mlxRunnerPool = newMLXRunnerCache()

func startMLXRunner(ctx context.Context, modelName string) (*exec.Cmd, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("allocate port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	bin, err := mlxRunnerBinary()
	if err != nil {
		return nil, 0, err
	}

	args := []string{"--mlx-engine", "-model", modelName, "-port", strconv.Itoa(port)}

	// Determine Python path
	pythonPath := "python3"
	if p := os.Getenv("OLLAMA_PYTHON"); p != "" {
		pythonPath = p
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			// Priority 1: Application Support (Ollmlx.app standard)
			appSupport := filepath.Join(home, "Library", "Application Support", "Ollmlx", "venv", "bin", "python3")
			// Priority 2: Dotfile (Legacy/Dev)
			dotFile := filepath.Join(home, ".ollmlx", "venv", "bin", "python3")

			if _, err := os.Stat(appSupport); err == nil {
				pythonPath = appSupport
			} else if _, err := os.Stat(dotFile); err == nil {
				pythonPath = dotFile
			} else {
				// Bootstrap?
				// If we are in an App Bundle, we should try to bootstrap the venv in Application Support
				exe, _ := os.Executable()
				resourcesReqs := filepath.Join(filepath.Dir(exe), "../Resources/mlx_backend/requirements.txt")
				if _, err := os.Stat(resourcesReqs); err == nil {
					// We are likely in an App Bundle and have requirements available
					slog.Info("bootstrapping python environment in Application Support", "requirements", resourcesReqs)
					venvDir := filepath.Join(home, "Library", "Application Support", "Ollmlx", "venv")
					
					// 1. Create venv
					if err := exec.Command("python3", "-m", "venv", venvDir).Run(); err == nil {
						// 2. Install deps
						pip := filepath.Join(venvDir, "bin", "pip")
						if err := exec.Command(pip, "install", "-r", resourcesReqs).Run(); err == nil {
							pythonPath = filepath.Join(venvDir, "bin", "python3")
							slog.Info("bootstrap complete", "python", pythonPath)
						} else {
							slog.Error("failed to install dependencies during bootstrap")
						}
					} else {
						slog.Error("failed to create venv during bootstrap")
					}
				}
			}
		}
	}

	// append python path arg
	args = append(args, "-python", pythonPath)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("OLLAMA_MODELS=%s", envconfig.Models()))
	return cmd, port, nil
}

func mlxRunnerBinary() (string, error) {
	if override := envconfig.Var("OLLAMA_MLX_RUNNER"); override != "" {
		if info, err := os.Stat(override); err == nil && info.Mode().IsRegular() {
			return override, nil
		} else if err != nil {
			return "", fmt.Errorf("invalid OLLAMA_MLX_RUNNER path: %w", err)
		}
	}

	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}

	wd, _ := os.Getwd()

	candidates := []string{
		filepath.Join(exeDir, "ollama-runner"),
		filepath.Join(exeDir, "mlxrunner"),
		filepath.Join(exeDir, "../Resources/ollama-runner"), // App Bundle
		filepath.Join(ml.LibOllamaPath, "ollama-runner"),
		filepath.Join(ml.LibOllamaPath, "mlxrunner"),
		filepath.Join(wd, "bin", "ollama-runner"),
		filepath.Join(wd, "ollama-runner"),
	}

	for _, candidate := range candidates {
		options := []string{candidate}
		if runtime.GOOS == "windows" {
			options = append(options, candidate+".exe")
		}

		for _, path := range options {
			if path == "" {
				continue
			}
			// EvalSymlinks to handle potential links
			if validPath, err := filepath.EvalSymlinks(path); err == nil {
				if info, err := os.Stat(validPath); err == nil && info.Mode().IsRegular() {
					return validPath, nil
				}
			}
		}
	}

	if path, err := exec.LookPath("ollama-runner"); err == nil {
		return path, nil
	}

	if path, err := buildTempRunnerBinary(); err == nil {
		return path, nil
	} else {
		slog.Debug("failed to build temporary mlx runner", "error", err)
	}

	return "", fmt.Errorf("mlx runner binary not found; set OLLAMA_MLX_RUNNER to the runner path")
}

func findRepoRoot(start string) string {
	for dir := filepath.Clean(start); dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return ""
}

func buildTempRunnerBinary() (string, error) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}

	wd, _ := os.Getwd()

	root := findRepoRoot(exeDir)
	if root == "" {
		root = findRepoRoot(wd)
	}
	if root == "" {
		return "", fmt.Errorf("cannot locate repository root to build runner")
	}

	tmpDir, err := os.MkdirTemp("", "ollama-mlxrunner-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	out := filepath.Join(tmpDir, "ollama-runner")

	buildCmd := exec.Command("go", "build", "-o", out, "./cmd/runner")
	buildCmd.Dir = root
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return "", fmt.Errorf("build runner: %w", err)
	}

	return out, nil
}

func waitForMLXRunner(ctx context.Context, client *http.Client, port int) error {
	deadline := time.Now().Add(30 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}

		if err != nil {
			lastErr = fmt.Errorf("health check request failed: %w", err)
			slog.Warn("mlx runner health check request failed", "error", err)
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			message := strings.TrimSpace(string(body))
			if message == "" {
				message = resp.Status
			} else {
				message = fmt.Sprintf("%s: %s", resp.Status, message)
			}
			lastErr = fmt.Errorf("health check returned %s", message)
			slog.Warn("mlx runner health check returned non-200 status", "status", resp.Status, "message", message)
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w: %v", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return fmt.Errorf("mlx runner did not become healthy: %v", lastErr)
	}
	return fmt.Errorf("mlx runner did not become healthy")
}

func loadMLXModel(ctx context.Context, client *http.Client, port int, modelName string) error {
	body, _ := json.Marshal(map[string]string{"model": modelName})
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/load", port), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read MLX backend load response", "error", err)
			return fmt.Errorf("failed to read backend response: %w", err)
		}
		return fmt.Errorf("load failed: %s", strings.TrimSpace(string(msg)))
	}
	return nil
}

func streamMLXCompletion(ctx context.Context, c *gin.Context, client *http.Client, port int, req *api.GenerateRequest) error {
	requestBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/completion", port), "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read MLX backend completion response", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to read backend response"})
			return nil
		}
		c.AbortWithStatusJSON(resp.StatusCode, gin.H{"error": strings.TrimSpace(string(msg))})
		return nil
	}

	defer resp.Body.Close()
	c.Header("Content-Type", "application/x-ndjson")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)
	created := time.Now().UTC()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var chunk mlxCompletionChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}

		out := api.GenerateResponse{
			Model:      req.Model,
			CreatedAt:  created,
			Response:   chunk.Content,
			Done:       chunk.Done,
			DoneReason: chunk.DoneReason,
			Metrics: api.Metrics{
				PromptEvalCount:    chunk.PromptEvalCount,
				PromptEvalDuration: chunk.PromptEvalDuration,
				EvalCount:          chunk.EvalCount,
				EvalDuration:       chunk.EvalDuration,
			},
		}

		line, _ := json.Marshal(out)
		c.Writer.Write(line)
		c.Writer.Write([]byte("\n"))
		if flusher != nil {
			flusher.Flush()
		}

		if chunk.Done {
			break
		}
	}

	return scanner.Err()
}

func streamMLXChat(ctx context.Context, c *gin.Context, client *http.Client, port int, req *api.ChatRequest, genReq *api.GenerateRequest) ([]api.ToolCall, error) {
	requestBody, err := json.Marshal(genReq)
	if err != nil {
		return nil, err
	}

	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/completion", port), "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read MLX backend chat completion response", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to read backend response"})
			return nil, nil
		}
		c.AbortWithStatusJSON(resp.StatusCode, gin.H{"error": strings.TrimSpace(string(msg))})
		return nil, nil
	}

	defer resp.Body.Close()
	c.Header("Content-Type", "application/x-ndjson")
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	flusher, _ := c.Writer.(http.Flusher)
	created := time.Now().UTC()
	var full strings.Builder
	var detectedToolCalls []api.ToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var chunk mlxCompletionChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}

		full.WriteString(chunk.Content)
		contentOut := chunk.Content
		toolCalls := chunk.ToolCalls
		if len(toolCalls) == 0 && chunk.Done {
			if calls, ok := parseToolCallsFromText(full.String()); ok {
				toolCalls = calls
				detectedToolCalls = calls
				// FIX: Extract non-JSON content to preserve reasoning/text
				// Only clear content if it's purely JSON, otherwise keep it
				fullText := full.String()
				jsonStart := strings.Index(fullText, "{")
				if jsonStart > 0 {
					// There's text before the JSON - preserve it
					contentOut = strings.TrimSpace(fullText[:jsonStart])
				} else {
					// Content is just JSON tool call, no need to show it again
					contentOut = ""
				}
			}
		} else if len(toolCalls) > 0 {
			// Tool calls came from backend - preserve any reasoning text
			detectedToolCalls = toolCalls
			// Don't clear contentOut - let the model's response text through
		}

		respMsg := api.Message{Role: "assistant", Content: contentOut, ToolCalls: toolCalls}
		chatResp := api.ChatResponse{
			Model:      req.Model,
			CreatedAt:  created,
			Message:    respMsg,
			Done:       chunk.Done,
			DoneReason: chunk.DoneReason,
			Metrics: api.Metrics{
				PromptEvalCount:    chunk.PromptEvalCount,
				PromptEvalDuration: chunk.PromptEvalDuration,
				EvalCount:          chunk.EvalCount,
				EvalDuration:       chunk.EvalDuration,
			},
		}

		line, _ := json.Marshal(chatResp)
		c.Writer.Write(line)
		c.Writer.Write([]byte("\n"))
		if flusher != nil {
			flusher.Flush()
		}

		if chunk.Done {
			break
		}
	}

	return detectedToolCalls, scanner.Err()
}

func collectMLXCompletion(ctx context.Context, client *http.Client, port int, req *api.GenerateRequest) (*api.GenerateResponse, error) {
	requestBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/completion", port), "application/json", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read MLX backend completion response", "error", err)
			return nil, fmt.Errorf("failed to read backend response: %w", err)
		}
		return nil, fmt.Errorf("completion failed: %s", strings.TrimSpace(string(msg)))
	}

	scanner := bufio.NewScanner(resp.Body)
	created := time.Now().UTC()
	var buf strings.Builder
	var last mlxCompletionChunk
	var toolCalls []api.ToolCall

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := json.Unmarshal(scanner.Bytes(), &last); err != nil {
			continue
		}
		if len(last.ToolCalls) > 0 {
			toolCalls = last.ToolCalls
		}
		buf.WriteString(last.Content)
		if last.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(toolCalls) > 0 && buf.Len() == 0 {
		if data, err := json.Marshal(map[string]any{"tool_calls": toolCalls}); err == nil {
			buf.Write(data)
		}
	}

	return &api.GenerateResponse{
		Model:      req.Model,
		CreatedAt:  created,
		Response:   buf.String(),
		Done:       true,
		DoneReason: last.DoneReason,
		Metrics: api.Metrics{
			PromptEvalCount:    last.PromptEvalCount,
			PromptEvalDuration: last.PromptEvalDuration,
			EvalCount:          last.EvalCount,
			EvalDuration:       last.EvalDuration,
		},
	}, nil
}

// executeToolCalls will synchronously execute provided tool calls using the
// tool definitions supplied in the request. Currently supports HTTP-style
// tools where the tool's Items is either a string URL or a map containing a
// "url" key. The function returns a human-readable aggregation of tool outputs.
func executeToolCalls(ctx context.Context, tools api.Tools, calls []api.ToolCall) (string, error) {
	var sb strings.Builder
	for _, call := range calls {
		name := call.Function.Name
		var tool *api.Tool
		for i := range tools {
			if tools[i].Function.Name == name {
				tool = &tools[i]
				break
			}
		}
		if tool == nil {
			return "", fmt.Errorf("tool %s not found", name)
		}

		// Determine endpoint URL for the tool. Support either a string or
		// an object with a "url" field in Items.
		var url string
		switch v := tool.Items.(type) {
		case string:
			url = v
		case map[string]any:
			if u, ok := v["url"].(string); ok {
				url = u
			}
		default:
			// attempt to marshal/unmarshal to map to be tolerant
			var maybe map[string]any
			b, _ := json.Marshal(v)
			_ = json.Unmarshal(b, &maybe)
			if u, ok := maybe["url"].(string); ok {
				url = u
			}
		}

		if url == "" {
			return "", fmt.Errorf("tool %s has no url configured in Items", name)
		}

		// Prepare HTTP request with the arguments as JSON
		body, _ := json.Marshal(call.Function.Arguments)
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("create request for tool %s: %w", name, err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("tool %s request failed: %w", name, err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		sb.WriteString(fmt.Sprintf("Tool %s response:\n%s\n\n", name, strings.TrimSpace(string(respBody))))
	}
	return sb.String(), nil
}

func toolPromptBlock(tools api.Tools) string {
	if len(tools) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("You have access to tools. When you need to call a function, output JSON in this format:\n")
	b.WriteString("{\"tool_calls\":[{\"name\":\"function_name\",\"arguments\":{...}}]}\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range tools {
		b.WriteString("- ")
		b.WriteString(t.Function.Name)
		b.WriteString(": ")
		b.WriteString(t.Function.Description)
		if len(t.Function.Parameters.Properties) > 0 {
			b.WriteString(". Parameters: ")
			first := true
			for name := range t.Function.Parameters.Properties {
				if !first {
					b.WriteString(", ")
				}
				b.WriteString(name)
				first = false
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ChatTemplateType represents different chat template formats
type ChatTemplateType string

const (
	TemplateQwen      ChatTemplateType = "qwen"      // Qwen, Qwen2, Qwen2.5
	TemplateLlama     ChatTemplateType = "llama"     // Llama 2, Llama 3, Code Llama
	TemplateMistral   ChatTemplateType = "mistral"   // Mistral, Mixtral
	TemplatePhi       ChatTemplateType = "phi"       // Phi-2, Phi-3
	TemplateGemma     ChatTemplateType = "gemma"     // Gemma, Gemma 2
	TemplateChatML    ChatTemplateType = "chatml"    // ChatML format (fallback)
	TemplateSmolLM    ChatTemplateType = "smollm"    // SmolLM models
	TemplateDefault   ChatTemplateType = "default"   // Generic fallback
)

// detectMLXChatTemplate determines the appropriate chat template based on model name
func detectMLXChatTemplate(modelName string) ChatTemplateType {
	lower := strings.ToLower(modelName)

	// Qwen family (Qwen, Qwen2, Qwen2.5, etc.)
	if strings.Contains(lower, "qwen") {
		return TemplateQwen
	}

	// Llama family
	if strings.Contains(lower, "llama") {
		// Llama 3 uses a different format
		if strings.Contains(lower, "llama-3") || strings.Contains(lower, "llama3") {
			return TemplateLlama
		}
		return TemplateLlama
	}

	// Mistral/Mixtral
	if strings.Contains(lower, "mistral") || strings.Contains(lower, "mixtral") {
		return TemplateMistral
	}

	// Phi models
	if strings.Contains(lower, "phi") {
		return TemplatePhi
	}

	// Gemma models
	if strings.Contains(lower, "gemma") {
		return TemplateGemma
	}

	// SmolLM models
	if strings.Contains(lower, "smollm") {
		return TemplateSmolLM
	}

	// Default to ChatML (widely supported)
	return TemplateChatML
}

// getImageToken returns the appropriate image token for a model
func getImageToken(modelName string, imageIndex int) string {
	lower := strings.ToLower(modelName)

	// Qwen2-VL uses numbered image tokens
	if strings.Contains(lower, "qwen") && strings.Contains(lower, "vl") {
		return fmt.Sprintf("<image_%d>", imageIndex+1)
	}

	// LLaVA and most other VLMs use <image>
	if strings.Contains(lower, "llava") || strings.Contains(lower, "pixtral") {
		return "<image>"
	}

	// Paligemma uses <image>
	if strings.Contains(lower, "paligemma") {
		return "<image>"
	}

	// Idefics uses <image>
	if strings.Contains(lower, "idefics") {
		return "<image>"
	}

	// Default to <image>
	return "<image>"
}

func formatChatPrompt(messages []api.Message, tools api.Tools) string {
	return formatChatPromptWithModel(messages, tools, "")
}

func formatChatPromptWithModel(messages []api.Message, tools api.Tools, modelName string) string {
	template := detectMLXChatTemplate(modelName)

	switch template {
	case TemplateQwen:
		return formatQwenPrompt(messages, tools, modelName)
	case TemplateLlama:
		return formatLlamaPrompt(messages, tools, modelName)
	case TemplateMistral:
		return formatMistralPrompt(messages, tools, modelName)
	case TemplatePhi:
		return formatPhiPrompt(messages, tools, modelName)
	case TemplateGemma:
		return formatGemmaPrompt(messages, tools, modelName)
	case TemplateSmolLM:
		return formatSmolLMPrompt(messages, tools, modelName)
	default:
		return formatChatMLPrompt(messages, tools, modelName)
	}
}

// formatQwenPrompt formats messages using Qwen's chat template
func formatQwenPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	// System message with tools
	b.WriteString("<|im_start|>system\nYou are a helpful assistant.")
	if len(tools) > 0 {
		b.WriteString(" ")
		b.WriteString(toolPromptBlock(tools))
	}
	b.WriteString("<|im_end|>\n")

	// User/assistant messages
	for _, m := range messages {
		b.WriteString("<|im_start|>")
		b.WriteString(m.Role)
		b.WriteString("\n")
		// Add image placeholders for vision models
		for i := range m.Images {
			b.WriteString(getImageToken(modelName, i))
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
		b.WriteString("<|im_end|>\n")
	}

	// Start assistant response
	b.WriteString("<|im_start|>assistant\n")

	return b.String()
}

// formatLlamaPrompt formats messages using Llama's chat template
func formatLlamaPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder
	lower := strings.ToLower(modelName)
	isLlama3 := strings.Contains(lower, "llama-3") || strings.Contains(lower, "llama3")

	if isLlama3 {
		// Llama 3 format
		b.WriteString("<|begin_of_text|>")

		// System message
		b.WriteString("<|start_header_id|>system<|end_header_id|>\n\n")
		b.WriteString("You are a helpful assistant.")
		if len(tools) > 0 {
			b.WriteString(" ")
			b.WriteString(toolPromptBlock(tools))
		}
		b.WriteString("<|eot_id|>")

		// User/assistant messages
		for _, m := range messages {
			b.WriteString("<|start_header_id|>")
			b.WriteString(m.Role)
			b.WriteString("<|end_header_id|>\n\n")
			for i := range m.Images {
				b.WriteString(getImageToken(modelName, i))
				b.WriteString("\n")
			}
			b.WriteString(m.Content)
			b.WriteString("<|eot_id|>")
		}

		// Start assistant response
		b.WriteString("<|start_header_id|>assistant<|end_header_id|>\n\n")
	} else {
		// Llama 2 format
		b.WriteString("[INST] <<SYS>>\nYou are a helpful assistant.")
		if len(tools) > 0 {
			b.WriteString(" ")
			b.WriteString(toolPromptBlock(tools))
		}
		b.WriteString("\n<</SYS>>\n\n")

		// Build conversation
		for i, m := range messages {
			if m.Role == "user" {
				if i > 0 {
					b.WriteString("[INST] ")
				}
				for j := range m.Images {
					b.WriteString(getImageToken(modelName, j))
					b.WriteString("\n")
				}
				b.WriteString(m.Content)
				b.WriteString(" [/INST]")
			} else if m.Role == "assistant" {
				b.WriteString(" ")
				b.WriteString(m.Content)
				b.WriteString(" </s><s>")
			}
		}
	}

	return b.String()
}

// formatMistralPrompt formats messages using Mistral's chat template
func formatMistralPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	b.WriteString("<s>")

	// Combine system message with first user message if present
	sysMsg := "You are a helpful assistant."
	if len(tools) > 0 {
		sysMsg += " " + toolPromptBlock(tools)
	}

	firstUser := true
	for _, m := range messages {
		if m.Role == "user" {
			b.WriteString("[INST] ")
			if firstUser {
				b.WriteString(sysMsg)
				b.WriteString("\n\n")
				firstUser = false
			}
			for i := range m.Images {
				b.WriteString(getImageToken(modelName, i))
				b.WriteString("\n")
			}
			b.WriteString(m.Content)
			b.WriteString(" [/INST]")
		} else if m.Role == "assistant" {
			b.WriteString(m.Content)
			b.WriteString("</s>")
		}
	}

	return b.String()
}

// formatPhiPrompt formats messages using Phi's chat template
func formatPhiPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	// System message
	b.WriteString("<|system|>\nYou are a helpful assistant.")
	if len(tools) > 0 {
		b.WriteString(" ")
		b.WriteString(toolPromptBlock(tools))
	}
	b.WriteString("<|end|>\n")

	// User/assistant messages
	for _, m := range messages {
		b.WriteString("<|")
		b.WriteString(m.Role)
		b.WriteString("|>\n")
		for i := range m.Images {
			b.WriteString(getImageToken(modelName, i))
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
		b.WriteString("<|end|>\n")
	}

	// Start assistant response
	b.WriteString("<|assistant|>\n")

	return b.String()
}

// formatGemmaPrompt formats messages using Gemma's chat template
func formatGemmaPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	// Gemma uses a simpler format
	for _, m := range messages {
		if m.Role == "user" {
			b.WriteString("<start_of_turn>user\n")
			for i := range m.Images {
				b.WriteString(getImageToken(modelName, i))
				b.WriteString("\n")
			}
			b.WriteString(m.Content)
			if len(tools) > 0 {
				b.WriteString("\n\n")
				b.WriteString(toolPromptBlock(tools))
			}
			b.WriteString("<end_of_turn>\n")
		} else if m.Role == "assistant" {
			b.WriteString("<start_of_turn>model\n")
			b.WriteString(m.Content)
			b.WriteString("<end_of_turn>\n")
		}
	}

	// Start model response
	b.WriteString("<start_of_turn>model\n")

	return b.String()
}

// formatSmolLMPrompt formats messages using SmolLM's chat template
func formatSmolLMPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	// SmolLM uses ChatML-like format
	b.WriteString("<|im_start|>system\nYou are a helpful AI assistant.")
	if len(tools) > 0 {
		b.WriteString(" ")
		b.WriteString(toolPromptBlock(tools))
	}
	b.WriteString("<|im_end|>\n")

	for _, m := range messages {
		b.WriteString("<|im_start|>")
		b.WriteString(m.Role)
		b.WriteString("\n")
		for i := range m.Images {
			b.WriteString(getImageToken(modelName, i))
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
		b.WriteString("<|im_end|>\n")
	}

	b.WriteString("<|im_start|>assistant\n")

	return b.String()
}

// formatChatMLPrompt is the default fallback using ChatML format
func formatChatMLPrompt(messages []api.Message, tools api.Tools, modelName string) string {
	var b strings.Builder

	// System message with tools
	b.WriteString("<|im_start|>system\nYou are a helpful assistant.")
	if len(tools) > 0 {
		b.WriteString(" ")
		b.WriteString(toolPromptBlock(tools))
	}
	b.WriteString("<|im_end|>\n")

	// User/assistant messages
	for _, m := range messages {
		b.WriteString("<|im_start|>")
		b.WriteString(m.Role)
		b.WriteString("\n")
		// Add image placeholders for vision models
		for i := range m.Images {
			b.WriteString(getImageToken(modelName, i))
			b.WriteString("\n")
		}
		b.WriteString(m.Content)
		b.WriteString("<|im_end|>\n")
	}

	// Start assistant response
	b.WriteString("<|im_start|>assistant\n")

	return b.String()
}

// extractImagesFromMessages collects all images from chat messages
func extractImagesFromMessages(messages []api.Message) []api.ImageData {
	var images []api.ImageData
	for _, m := range messages {
		images = append(images, m.Images...)
	}
	return images
}

func parseToolCallsFromText(text string) ([]api.ToolCall, bool) {
	text = strings.TrimSpace(text)

	// Try to find JSON in the text (models often add extra text around JSON)
	var jsonCandidates []string
	depth := 0
	start := -1
	for i, c := range text {
		if c == '{' || c == '[' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if c == '}' || c == ']' {
			depth--
			if depth == 0 && start >= 0 {
				jsonCandidates = append(jsonCandidates, text[start:i+1])
				start = -1
			}
		}
	}
	if len(jsonCandidates) == 0 {
		jsonCandidates = []string{text}
	}

	for _, candidate := range jsonCandidates {
		// Format 1: {"tool_calls": [{"name": ..., "arguments": ...}]} (simple format from models)
		var simpleEnvelope struct {
			ToolCalls []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(candidate), &simpleEnvelope); err == nil && len(simpleEnvelope.ToolCalls) > 0 {
			var result []api.ToolCall
			for _, tc := range simpleEnvelope.ToolCalls {
				if tc.Name != "" {
					result = append(result, api.ToolCall{
						Function: api.ToolCallFunction{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					})
				}
			}
			if len(result) > 0 {
				return result, true
			}
		}

		// Format 2: {"tool_calls": [{"function": {...}}]} (OpenAI format)
		var envelope struct {
			ToolCalls []api.ToolCall `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(candidate), &envelope); err == nil && len(envelope.ToolCalls) > 0 {
			hasValidCall := false
			for _, tc := range envelope.ToolCalls {
				if tc.Function.Name != "" {
					hasValidCall = true
					break
				}
			}
			if hasValidCall {
				return envelope.ToolCalls, true
			}
		}

		// Format 3: Direct array of tool calls
		var directArray []api.ToolCall
		if err := json.Unmarshal([]byte(candidate), &directArray); err == nil && len(directArray) > 0 {
			return directArray, true
		}

		// Format 4: Single tool call object
		var singleCall api.ToolCall
		if err := json.Unmarshal([]byte(candidate), &singleCall); err == nil && singleCall.Function.Name != "" {
			return []api.ToolCall{singleCall}, true
		}

		// Format 5: {"name": "fn", "arguments": {...}} (simple format)
		var simpleCall struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(candidate), &simpleCall); err == nil && simpleCall.Name != "" {
			tc := api.ToolCall{
				Function: api.ToolCallFunction{
					Name:      simpleCall.Name,
					Arguments: simpleCall.Arguments,
				},
			}
			return []api.ToolCall{tc}, true
		}

		// Format 6: {"tool_name": {...}} - tool name as key
		var obj map[string]any
		if err := json.Unmarshal([]byte(candidate), &obj); err == nil && len(obj) == 1 {
			for name, args := range obj {
				if argsMap, ok := args.(map[string]any); ok {
					tc := api.ToolCall{
						Function: api.ToolCallFunction{
							Name:      name,
							Arguments: argsMap,
						},
					}
					return []api.ToolCall{tc}, true
				}
			}
		}
	}

	return nil, false
}

// ListMLXModels returns all locally cached MLX models
func ListMLXModels() ([]api.ListModelResponse, error) {
	manager := llm.NewMLXModelManager()

	mlxModels, err := manager.ListModels()
	if err != nil {
		return nil, err
	}

	var models []api.ListModelResponse
	for _, m := range mlxModels {
		models = append(models, api.ListModelResponse{
			Model:      m.Name,
			Name:       m.Name,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
			Details: api.ModelDetails{
				Format:            "MLX",
				Family:            m.Family,
				ParameterSize:     m.ParameterSize,
				QuantizationLevel: m.QuantizLevel,
			},
		})
	}

	return models, nil
}

// ShowMLXModel returns metadata for a specific MLX model
func ShowMLXModel(modelName string) (*api.ShowResponse, error) {
	manager := llm.NewMLXModelManager()

	// Convert HuggingFace URL format to local directory name
	localName := strings.ReplaceAll(modelName, "/", "_")

	info, err := manager.GetModelInfo(localName)
	if err != nil {
		return nil, err
	}

	return &api.ShowResponse{
		ModelInfo: map[string]any{
			"general.architecture":       "mlx",
			"general.family":             info.Family,
			"general.parameter_count":    float64(parseParameterCount(info.ParameterSize)),
			"general.quantization_level": info.QuantizLevel,
		},
		ModifiedAt: info.ModifiedAt,
		Details: api.ModelDetails{
			Format:            "MLX",
			Family:            info.Family,
			ParameterSize:     info.ParameterSize,
			QuantizationLevel: info.QuantizLevel,
		},
	}, nil
}

// DeleteMLXModel removes an MLX model from local storage
func DeleteMLXModel(modelName string) error {
	manager := llm.NewMLXModelManager()
	return manager.DeleteModel(modelName)
}

// IsMLXModelReference checks if a model name is an MLX model reference
func IsMLXModelReference(modelName string) bool {
	// MLX models typically come from HuggingFace with format:
	// - "mlx-community/ModelName"
	// - contain "mlx" in the name
	// - or are stored in the MLX models directory

	if strings.HasPrefix(modelName, "mlx-community/") {
		return true
	}

	if strings.Contains(strings.ToLower(modelName), "-mlx") {
		return true
	}

	// Check if model exists in MLX cache
	manager := llm.NewMLXModelManager()
	return manager.ModelExists(modelName)
}

// generateMLXModel handles generation requests for MLX models
func (s *Server) generateMLXModel(c *gin.Context, req *api.GenerateRequest) {
	ctx := c.Request.Context()
	manager := llm.NewMLXModelManager()
	modelName := req.Model
	localName := strings.ReplaceAll(modelName, "/", "_")
	keepAlive := 5 * time.Minute
	if req.KeepAlive != nil {
		keepAlive = req.KeepAlive.Duration
	}

	if !manager.ModelExists(modelName) {
		slog.Info("MLX model missing locally, downloading from Hugging Face", "model", modelName)

		downloadCtx, cancel := context.WithCancel(ctx)
		defer cancel()

	if err := manager.DownloadMLXModel(downloadCtx, modelName, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to download MLX model: %v", err)})
			return
		}
	}

	if _, err := manager.GetModelInfo(modelName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	entry, err := mlxRunnerPool.getRunner(ctx, localName, keepAlive)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to provision MLX runner: %v", err)})
		return
	}
	if keepAlive == 0 {
		defer mlxRunnerPool.evict(localName)
	} else {
		defer mlxRunnerPool.touch(localName)
	}

	client := entry.client
	port := entry.port

	if req.Stream != nil && !*req.Stream {
		resp, err := collectMLXCompletion(ctx, client, port, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, context.Canceled) {
				status = http.StatusRequestTimeout
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		// For /api/generate, we just return the response as-is.
		// Tool call JSON will be in the response if the model generated it.
		c.JSON(http.StatusOK, resp)
		return
	}

	if err := streamMLXCompletion(ctx, c, client, port, req); err != nil && !errors.Is(err, context.Canceled) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (s *Server) chatMLXModel(c *gin.Context, req *api.ChatRequest) {
	ctx := c.Request.Context()
	manager := llm.NewMLXModelManager()
	localName := strings.ReplaceAll(req.Model, "/", "_")
	keepAlive := 5 * time.Minute
	if req.KeepAlive != nil {
		keepAlive = req.KeepAlive.Duration
	}

	if !manager.ModelExists(localName) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("model '%s' not found", req.Model)})
		return
	}

	if _, err := manager.GetModelInfo(localName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	entry, err := mlxRunnerPool.getRunner(ctx, localName, keepAlive)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to provision MLX runner: %v", err)})
		return
	}
	if keepAlive == 0 {
		defer mlxRunnerPool.evict(localName)
	} else {
		defer mlxRunnerPool.touch(localName)
	}

	client := entry.client
	port := entry.port

	stream := true
	if req.Stream != nil {
		stream = *req.Stream
	}

	prompt := formatChatPromptWithModel(req.Messages, req.Tools, req.Model)
	images := extractImagesFromMessages(req.Messages)
	genReq := &api.GenerateRequest{
		Model:     req.Model,
		Prompt:    prompt,
		Stream:    &stream,
		Format:    req.Format,
		KeepAlive: req.KeepAlive,
		Options:   req.Options,
		Tools:     req.Tools,
		Images:    images,
	}

	if stream {
		toolCalls, err := streamMLXChat(ctx, c, client, port, req, genReq)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		// Note: For streaming, tool calls are already included in the final chunk
		// We log them here for debugging but the client already received them
		if len(toolCalls) > 0 && len(req.Tools) > 0 {
			slog.Debug("streaming detected tool calls", "count", len(toolCalls))
		}
		return
	}

	resp, err := collectMLXCompletion(ctx, client, port, genReq)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	// If the model requested tool calls, return them to the client.
	// Standard Ollama behavior is to return tool calls for the client to handle.
	if len(req.Tools) > 0 {
		if toolCalls, ok := parseToolCallsFromText(resp.Response); ok && len(toolCalls) > 0 {
			// Return the tool calls in the message - client handles execution
			message := api.Message{
				Role:      "assistant",
				Content:   "", // Clear content when we have tool calls
				ToolCalls: toolCalls,
			}
			chatResp := api.ChatResponse{
				Model:      req.Model,
				CreatedAt:  resp.CreatedAt,
				Message:    message,
				Done:       true,
				DoneReason: "tool_calls",
				Metrics:    resp.Metrics,
			}
			c.JSON(http.StatusOK, chatResp)
			return
		}
	}

	message := api.Message{Role: "assistant", Content: resp.Response}

	chatResp := api.ChatResponse{
		Model:      req.Model,
		CreatedAt:  resp.CreatedAt,
		Message:    message,
		Done:       resp.Done,
		DoneReason: resp.DoneReason,
		Metrics:    resp.Metrics,
	}

	c.JSON(http.StatusOK, chatResp)
}

// parseParameterCount converts parameter size string to number
// Supports formats like: "7b", "7B", "1.5b", "135m", "135M", "7 billion", "1,000,000,000"
func parseParameterCount(paramSize string) int64 {
	paramSize = strings.ToLower(strings.TrimSpace(paramSize))
	if paramSize == "" {
		return 0
	}

	// Remove commas and spaces
	paramSize = strings.ReplaceAll(paramSize, ",", "")
	paramSize = strings.ReplaceAll(paramSize, " ", "")

	// Handle word suffixes first
	if strings.HasSuffix(paramSize, "billion") {
		numStr := strings.TrimSuffix(paramSize, "billion")
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int64(val * 1_000_000_000)
		}
	}
	if strings.HasSuffix(paramSize, "million") {
		numStr := strings.TrimSuffix(paramSize, "million")
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int64(val * 1_000_000)
		}
	}
	if strings.HasSuffix(paramSize, "thousand") || strings.HasSuffix(paramSize, "k") {
		numStr := strings.TrimSuffix(paramSize, "thousand")
		numStr = strings.TrimSuffix(numStr, "k")
		if val, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int64(val * 1_000)
		}
	}

	// Handle short suffixes: b (billion), m (million), k (thousand)
	var multiplier float64 = 1

	if strings.HasSuffix(paramSize, "b") {
		paramSize = strings.TrimSuffix(paramSize, "b")
		multiplier = 1_000_000_000
	} else if strings.HasSuffix(paramSize, "m") {
		paramSize = strings.TrimSuffix(paramSize, "m")
		multiplier = 1_000_000
	} else if strings.HasSuffix(paramSize, "t") {
		// "t" for trillion (rare but possible)
		paramSize = strings.TrimSuffix(paramSize, "t")
		multiplier = 1_000_000_000_000
	}

	// Try to parse the numeric part
	if val, err := strconv.ParseFloat(paramSize, 64); err == nil {
		return int64(val * multiplier)
	}

	// If we can't parse it, try to extract any numbers we can find
	// This handles cases like "Llama-3-70B" -> 70B
	var numBuilder strings.Builder
	var foundDot bool
	for _, c := range paramSize {
		if c >= '0' && c <= '9' {
			numBuilder.WriteRune(c)
		} else if c == '.' && !foundDot {
			numBuilder.WriteRune(c)
			foundDot = true
		}
	}

	if numBuilder.Len() > 0 {
		if val, err := strconv.ParseFloat(numBuilder.String(), 64); err == nil {
			// If multiplier is still 1, make an educated guess based on value
			if multiplier == 1 {
				if val < 1000 {
					// Probably in billions (e.g., "70" means 70B)
					multiplier = 1_000_000_000
				}
			}
			return int64(val * multiplier)
		}
	}

	return 0
}

// mlxEmbeddingResponse is the response from the MLX backend embedding endpoint
type mlxEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model"`
}

// EmbedMLXModel generates embeddings using an MLX model
func (s *Server) EmbedMLXModel(c *gin.Context, modelName string, input []string) ([][]float32, error) {
	ctx := c.Request.Context()
	manager := llm.NewMLXModelManager()
	localName := strings.ReplaceAll(modelName, "/", "_")
	keepAlive := 5 * time.Minute

	if !manager.ModelExists(localName) {
		return nil, fmt.Errorf("model '%s' not found", modelName)
	}

	entry, err := mlxRunnerPool.getRunner(ctx, localName, keepAlive)
	if err != nil {
		return nil, fmt.Errorf("failed to provision MLX runner: %v", err)
	}
	defer mlxRunnerPool.touch(localName)

	client := entry.client
	port := entry.port

	// Generate embeddings for each input
	var allEmbeddings [][]float32
	for _, text := range input {
		reqBody, _ := json.Marshal(map[string]string{"prompt": text})
		resp, err := client.Post(
			fmt.Sprintf("http://127.0.0.1:%d/embedding", port),
			"application/json",
			bytes.NewReader(reqBody),
		)
		if err != nil {
			return nil, fmt.Errorf("embedding request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("embedding failed: %s", strings.TrimSpace(string(body)))
		}

		var embResp mlxEmbeddingResponse
		if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
			return nil, fmt.Errorf("failed to decode embedding response: %v", err)
		}

		allEmbeddings = append(allEmbeddings, embResp.Embeddings...)
	}

	return allEmbeddings, nil
}

