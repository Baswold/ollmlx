package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/ollama/ollama/api"
	"github.com/stretchr/testify/require"
)

func TestLegacyGenerateRequestAdapter(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	original := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(original) })

	router := gin.New()
	router.POST("/api/generate", func(c *gin.Context) {
		req, warnings, err := bindGenerateWithMLXCompat(c)
		require.NoError(t, err)
		emitMLXCompatWarnings(c, req.Model, warnings)
		c.JSON(http.StatusOK, req)
	})

	body := `{"model":"mlx-community_model","input":"hello from legacy","stream":false}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(body))

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "mlx-community/model", resp["model"])
	require.Equal(t, "hello from legacy", resp["prompt"])

	logs := logBuf.String()
	require.Contains(t, logs, "legacy MLX compatibility shim applied")
	require.Contains(t, logs, "legacy MLX model names")
	require.Contains(t, logs, "'input' field is deprecated")
}

func TestLegacyChatRequestAdapter(t *testing.T) {
	t.Setenv("GIN_MODE", gin.TestMode)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	original := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(original) })

	router := gin.New()
	router.POST("/api/chat", func(c *gin.Context) {
		req, warnings, err := bindChatWithMLXCompat(c)
		require.NoError(t, err)
		emitMLXCompatWarnings(c, req.Model, warnings)
		c.JSON(http.StatusOK, req)
	})

	body := `{"model":"mlx-community_model","prompt":"hi there"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Model    string        `json:"model"`
		Messages []api.Message `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "mlx-community/model", resp.Model)
	require.Len(t, resp.Messages, 1)
	require.Equal(t, "hi there", resp.Messages[0].Content)

	logs := logBuf.String()
	require.Contains(t, logs, "legacy MLX compatibility shim applied")
	require.Contains(t, logs, "using 'prompt' on the chat endpoint is deprecated")
}
