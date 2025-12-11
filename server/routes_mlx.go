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
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/llm"
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
	Content            string        `json:"content"`
	Done               bool          `json:"done"`
	DoneReason         string        `json:"done_reason"`
	PromptEvalCount    int           `json:"prompt_eval_count"`
	PromptEvalDuration time.Duration `json:"prompt_eval_duration"`
	EvalCount          int           `json:"eval_count"`
	EvalDuration       time.Duration `json:"eval_duration"`
	Logprobs           any           `json:"logprobs"`
}

func startMLXRunner(ctx context.Context, modelName string) (*exec.Cmd, int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("allocate port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	cmd := exec.CommandContext(ctx, "go", "run", "./runner/mlxrunner/runner.go", "-model", modelName, "-port", strconv.Itoa(port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd, port, nil
}

func waitForMLXRunner(ctx context.Context, client *http.Client, port int) error {
	deadline := time.Now().Add(30 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
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

func streamMLXChat(ctx context.Context, c *gin.Context, client *http.Client, port int, req *api.ChatRequest, genReq *api.GenerateRequest) error {
	requestBody, err := json.Marshal(genReq)
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
			slog.Error("failed to read MLX backend chat completion response", "error", err)
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
	var full strings.Builder

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

		full.WriteString(chunk.Content)
		contentOut := chunk.Content
		var toolCalls []api.ToolCall
		if chunk.Done {
			if calls, ok := parseToolCallsFromText(full.String()); ok {
				toolCalls = calls
				contentOut = ""
			}
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

	return scanner.Err()
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

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := json.Unmarshal(scanner.Bytes(), &last); err != nil {
			continue
		}
		buf.WriteString(last.Content)
		if last.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
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

	runnerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd, port, err := startMLXRunner(runnerCtx, req.Model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start MLX runner: %v", err)})
		return
	}

	if err := cmd.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to launch MLX runner: %v", err)})
		return
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	client := &http.Client{Timeout: 5 * time.Minute}
	if err := waitForMLXRunner(runnerCtx, client, port); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MLX runner not ready: %v", err)})
		return
	}

	if err := loadMLXModel(runnerCtx, client, port, req.Model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Stream != nil && !*req.Stream {
		resp, err := collectMLXCompletion(runnerCtx, client, port, req)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, context.Canceled) {
				status = http.StatusRequestTimeout
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	if err := streamMLXCompletion(runnerCtx, c, client, port, req); err != nil && !errors.Is(err, context.Canceled) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (s *Server) chatMLXModel(c *gin.Context, req *api.ChatRequest) {
	ctx := c.Request.Context()
	manager := llm.NewMLXModelManager()
	localName := strings.ReplaceAll(req.Model, "/", "_")

	if !manager.ModelExists(localName) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("model '%s' not found", req.Model)})
		return
	}

	if _, err := manager.GetModelInfo(localName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	runnerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd, port, err := startMLXRunner(runnerCtx, req.Model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start MLX runner: %v", err)})
		return
	}

	if err := cmd.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to launch MLX runner: %v", err)})
		return
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	client := &http.Client{Timeout: 5 * time.Minute}
	if err := waitForMLXRunner(runnerCtx, client, port); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("MLX runner not ready: %v", err)})
		return
	}

	if err := loadMLXModel(runnerCtx, client, port, req.Model); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
	}

	if stream {
		err := streamMLXChat(runnerCtx, c, client, port, req, genReq)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	resp, err := collectMLXCompletion(runnerCtx, client, port, genReq)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	message := api.Message{Role: "assistant", Content: resp.Response}
	if len(req.Tools) > 0 {
		if toolCalls, ok := parseToolCallsFromText(resp.Response); ok {
			message.ToolCalls = toolCalls
			message.Content = ""
		}
	}

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
