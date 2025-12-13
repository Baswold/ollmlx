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
	"math"
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

// PullMLXModel downloads an MLX model from HuggingFace
func PullMLXModel(ctx context.Context, modelName string, fn func(api.ProgressResponse)) error {
	slog.Info("pulling MLX model from HuggingFace", "model", modelName)

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
		Status: fmt.Sprintf("pulling MLX model %s from HuggingFace", modelName),
	})

	err := manager.DownloadMLXModel(ctx, modelName, func(status string, progress float64) {
		fn(api.ProgressResponse{
			Status:    status,
			Digest:    digest,
			Completed: int64(math.Round(progress)),
			Total:     100,
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
		client:    &http.Client{Timeout: 5 * time.Minute},
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
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path, nil
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
				contentOut = ""
				detectedToolCalls = calls
			}
		} else if len(toolCalls) > 0 {
			contentOut = ""
			detectedToolCalls = toolCalls
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
	b.WriteString("\n\nYou can call tools by responding with JSON of the form {\"tool_calls\": [{\"name\": \"tool_name\", \"arguments\": {...}}]}\n")
	for _, t := range tools {
		b.WriteString("Tool: ")
		b.WriteString(t.Function.Name)
		b.WriteString("\nDescription: ")
		b.WriteString(t.Function.Description)
		if params, err := json.Marshal(t.Function.Parameters); err == nil {
			b.WriteString("\nParameters (JSON Schema): ")
			b.Write(params)
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func formatChatPrompt(messages []api.Message, tools api.Tools) string {
	var b strings.Builder
	b.WriteString("You are a helpful assistant.\n\n")
	for _, m := range messages {
		b.WriteString(strings.ToUpper(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	if len(tools) > 0 {
		b.WriteString(toolPromptBlock(tools))
		b.WriteString("If you need to use a tool, respond ONLY with the JSON tool_calls block. Otherwise, answer normally.\n")
	}

	return b.String()
}

func parseToolCallsFromText(text string) ([]api.ToolCall, bool) {
	var envelope struct {
		ToolCalls []api.ToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &envelope); err == nil && len(envelope.ToolCalls) > 0 {
		return envelope.ToolCalls, true
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

		if err := manager.DownloadMLXModel(downloadCtx, modelName, func(status string, progress float64) {
			slog.Info("downloading MLX model", "model", modelName, "status", status, "progress", progress)
		}); err != nil {
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

		// If the model returned tool calls and tools were provided, attempt to
		// execute them synchronously and re-run the model with the tool outputs
		// appended to the prompt (non-streaming flow only).
		if len(req.Tools) > 0 {
			if toolCalls, ok := parseToolCallsFromText(resp.Response); ok && len(toolCalls) > 0 {
				toolResults, err := executeToolCalls(ctx, req.Tools, toolCalls)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("tool execution failed: %v", err)})
					return
				}

				// Build a follow-up request including the tool outputs for the model
				followUp := *req
				followUp.Prompt = req.Prompt + "\n\nTool results:\n" + toolResults

				finalResp, err := collectMLXCompletion(ctx, client, port, &followUp)
				if err != nil {
					status := http.StatusInternalServerError
					if errors.Is(err, context.Canceled) {
						status = http.StatusRequestTimeout
					}
					c.JSON(status, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, finalResp)
				return
			}
		}

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

	prompt := formatChatPrompt(req.Messages, req.Tools)
	genReq := &api.GenerateRequest{
		Model:     req.Model,
		Prompt:    prompt,
		Stream:    &stream,
		Format:    req.Format,
		KeepAlive: req.KeepAlive,
		Options:   req.Options,
		Tools:     req.Tools,
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

	// If the model requested tool calls and we have tool definitions, execute
	// them and re-run the model with the tool outputs appended to the prompt.
	if len(req.Tools) > 0 {
		if toolCalls, ok := parseToolCallsFromText(resp.Response); ok && len(toolCalls) > 0 {
			toolResults, err := executeToolCalls(ctx, req.Tools, toolCalls)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("tool execution failed: %v", err)})
				return
			}
			// Re-run model with tool outputs appended to prompt
			follow := *genReq
			follow.Prompt = genReq.Prompt + "\n\nTool results:\n" + toolResults
			finalResp, err := collectMLXCompletion(ctx, client, port, &follow)
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, context.Canceled) {
					status = http.StatusRequestTimeout
				}
				c.JSON(status, gin.H{"error": err.Error()})
				return
			}
			message := api.Message{Role: "assistant", Content: finalResp.Response}
			chatResp := api.ChatResponse{
				Model:      req.Model,
				CreatedAt:  finalResp.CreatedAt,
				Message:    message,
				Done:       finalResp.Done,
				DoneReason: finalResp.DoneReason,
				Metrics:    finalResp.Metrics,
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
func parseParameterCount(paramSize string) int64 {
	paramSize = strings.ToLower(strings.TrimSpace(paramSize))

	// Handle common formats like "7b", "7 billion", "7,000,000,000"
	if strings.HasSuffix(paramSize, "b") {
		// Remove "b" suffix
		numStr := strings.TrimSuffix(paramSize, "b")

		// Handle "7b" format
		if numStr == "7" {
			return 7_000_000_000
		} else if numStr == "135m" {
			return 135_000_000
		} else if numStr == "1.7b" {
			return 1_700_000_000
		} else if numStr == "3b" {
			return 3_000_000_000
		} else if numStr == "1b" {
			return 1_000_000_000
		}
	}

	// Default to 0 if we can't parse it
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

