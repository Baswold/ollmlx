package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ollama/ollama/api"
)

// legacyMLXGeneratePayload captures legacy request shapes that were accepted by earlier
// MLX implementations. These fields are mapped onto the canonical GenerateRequest
// structure so callers do not break while transitioning to the new API shape.
type legacyMLXGeneratePayload struct {
	api.GenerateRequest
	Input    string        `json:"input"`
	Messages []api.Message `json:"messages"`
}

// legacyMLXChatPayload captures fields occasionally sent to the chat endpoint that do
// not align with the ChatRequest shape. These values are adapted into the modern struct
// and callers are warned that the legacy fields are deprecated.
type legacyMLXChatPayload struct {
	api.ChatRequest
	Prompt string `json:"prompt"`
	Input  string `json:"input"`
}

func bindGenerateWithMLXCompat(c *gin.Context) (api.GenerateRequest, []string, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return api.GenerateRequest{}, nil, err
	}

	// gin.ShouldBindJSON returns io.EOF on an empty body; mirror that behavior so the
	// caller can return the same HTTP status codes as the rest of the server.
	if len(bytes.TrimSpace(body)) == 0 {
		return api.GenerateRequest{}, nil, io.EOF
	}

	var req api.GenerateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return api.GenerateRequest{}, nil, err
	}

	var legacy legacyMLXGeneratePayload
	// Ignore errors here so valid modern requests still succeed.
	_ = json.Unmarshal(body, &legacy)

	warnings := applyLegacyMLXGenerateAdapters(&req, &legacy)
	return req, warnings, nil
}

func bindChatWithMLXCompat(c *gin.Context) (api.ChatRequest, []string, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return api.ChatRequest{}, nil, err
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return api.ChatRequest{}, nil, io.EOF
	}

	var req api.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return api.ChatRequest{}, nil, err
	}

	var legacy legacyMLXChatPayload
	_ = json.Unmarshal(body, &legacy)

	warnings := applyLegacyMLXChatAdapters(&req, &legacy)
	return req, warnings, nil
}

func applyLegacyMLXGenerateAdapters(req *api.GenerateRequest, legacy *legacyMLXGeneratePayload) []string {
	var warnings []string

	if req.Prompt == "" && legacy.Input != "" {
		req.Prompt = legacy.Input
		warnings = append(warnings, "the 'input' field is deprecated; use 'prompt'")
	}

	if req.Prompt == "" && len(legacy.Messages) > 0 {
		req.Prompt = formatChatPrompt(legacy.Messages, nil)
		warnings = append(warnings, "chat-style 'messages' were converted to a prompt; send 'prompt' instead")
	}

	if normalized, warn := normalizeLegacyMLXModelName(req.Model); warn {
		warnings = append(warnings, "legacy MLX model names using underscores are deprecated; use slash-separated names")
		req.Model = normalized
	}

	return warnings
}

func applyLegacyMLXChatAdapters(req *api.ChatRequest, legacy *legacyMLXChatPayload) []string {
	var warnings []string

	if len(req.Messages) == 0 && legacy.Prompt != "" {
		req.Messages = []api.Message{{Role: "user", Content: legacy.Prompt}}
		warnings = append(warnings, "using 'prompt' on the chat endpoint is deprecated; send 'messages' instead")
	}

	if len(req.Messages) == 0 && legacy.Input != "" {
		req.Messages = []api.Message{{Role: "user", Content: legacy.Input}}
		warnings = append(warnings, "the 'input' field is deprecated; send chat 'messages' instead")
	}

	if normalized, warn := normalizeLegacyMLXModelName(req.Model); warn {
		warnings = append(warnings, "legacy MLX model names using underscores are deprecated; use slash-separated names")
		req.Model = normalized
	}

	return warnings
}

func normalizeLegacyMLXModelName(model string) (string, bool) {
	if strings.HasPrefix(model, "mlx-community_") {
		return "mlx-community/" + strings.TrimPrefix(model, "mlx-community_"), true
	}

	return model, false
}

func emitMLXCompatWarnings(c *gin.Context, model string, warnings []string) {
	for _, w := range warnings {
		slog.Warn("legacy MLX compatibility shim applied", "path", c.FullPath(), "model", model, "warning", w)
	}
}
