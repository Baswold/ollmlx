package llm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ollama/ollama/envconfig"
)

// MLXModelInfo represents metadata about an MLX model
type MLXModelInfo struct {
	Name           string    `json:"name"`
	Size           int64     `json:"size"`
	Digest         string    `json:"digest"`
	ModifiedAt     time.Time `json:"modified_at"`
	Format         string    `json:"format"`
	Family         string    `json:"family"`
	ParameterSize  string    `json:"parameter_size"`
	QuantizLevel   string    `json:"quantization_level"`
	LocalPath      string    `json:"-"`
	HuggingFaceURL string    `json:"huggingface_url,omitempty"`
}

// MLXModelManager handles MLX model storage and retrieval
type MLXModelManager struct {
	modelsDir string
}

// hfModelInfo mirrors the subset of Hugging Face model metadata we need.
// The "siblings" list contains files at the repository root, which is sufficient
// for typical MLX model layouts (config, tokenizer, weights shards).
type hfModelInfo struct {
	Siblings []struct {
		RFilename string `json:"rfilename"`
		Size      int64  `json:"size"`
		LFS       struct {
			Size int64 `json:"size"`
		} `json:"lfs"`
	} `json:"siblings"`
}

// NewMLXModelManager creates a new MLX model manager
func NewMLXModelManager() *MLXModelManager {
	// Use ollmlx model directory (defaults to ~/.ollmlx/models)
	modelsDir := envconfig.Models()
	os.MkdirAll(modelsDir, 0755)

	return &MLXModelManager{
		modelsDir: modelsDir,
	}
}

// GetModelsDir returns the directory where MLX models are stored
func (m *MLXModelManager) GetModelsDir() string {
	return m.modelsDir
}

// internalDirs are directories used for Ollama compatibility, not MLX models
var internalDirs = map[string]bool{
	"blobs":     true,
	"manifests": true,
	"mlx":       true,
	"ollama":    true, // Ollama-format models stored in separate subfolder
}

// toDisplayName converts a filesystem name (with underscores) to display format (with slashes)
// e.g., "mlx-community_Qwen2.5-0.5B" -> "mlx-community/Qwen2.5-0.5B"
func toDisplayName(fsName string) string {
	// Only convert the first underscore after known org prefixes
	for _, prefix := range []string{"mlx-community_", "huggingface_", "meta-llama_", "mistralai_", "Qwen_", "google_", "microsoft_"} {
		if strings.HasPrefix(fsName, prefix) {
			org := strings.TrimSuffix(prefix, "_")
			model := strings.TrimPrefix(fsName, prefix)
			return org + "/" + model
		}
	}
	return fsName
}

// ListModels returns all locally cached MLX models
func (m *MLXModelManager) ListModels() ([]MLXModelInfo, error) {
	var models []MLXModelInfo

	entries, err := os.ReadDir(m.modelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return models, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip internal directories (blobs, manifests, mlx)
		if internalDirs[entry.Name()] {
			continue
		}

		// Validate this is actually a model (has config.json + weights)
		if !m.ModelExists(entry.Name()) {
			continue
		}

		modelPath := filepath.Join(m.modelsDir, entry.Name())
		info, err := m.GetModelInfo(entry.Name())
		if err != nil {
			// Skip invalid models
			continue
		}

		// Convert to display name (org/model format)
		info.Name = toDisplayName(entry.Name())
		info.LocalPath = modelPath
		models = append(models, info)
	}

	return models, nil
}

// GetModelInfo retrieves metadata for a specific MLX model
func (m *MLXModelManager) GetModelInfo(modelName string) (MLXModelInfo, error) {
	modelPath := m.GetModelPath(modelName)

	info := MLXModelInfo{
		Name:   modelName,
		Format: "MLX",
	}

	// Check if model exists
	stat, err := os.Stat(modelPath)
	if err != nil {
		return info, err
	}

	info.ModifiedAt = stat.ModTime()

	// Try to read config.json for metadata
	configPath := filepath.Join(modelPath, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err == nil {
			// Extract model family and size from config
			if arch, ok := config["architectures"].([]interface{}); ok && len(arch) > 0 {
				info.Family = fmt.Sprintf("%v", arch[0])
			}
			if hiddenSize, ok := config["hidden_size"].(float64); ok {
				// Rough estimate of parameter count from hidden size
				params := int(hiddenSize * 1000 / 1024) // Very rough approximation
				info.ParameterSize = fmt.Sprintf("%dM", params)
			}
		}
	}

	// Calculate total size
	size, err := m.calculateDirSize(modelPath)
	if err == nil {
		info.Size = size
	}

	// Generate a stable digest from file layout (fallback to name if it fails)
	if digest, err := computeDigest(modelPath); err == nil {
		info.Digest = digest
	} else {
		sum := sha256.Sum256([]byte(modelName))
		info.Digest = fmt.Sprintf("sha256:%x", sum)
	}

	return info, nil
}

// GetModelPath returns the local path for a model name
func (m *MLXModelManager) GetModelPath(modelName string) string {
	// Handle both simple names and HuggingFace-style names
	// e.g., "llama-3-8b" or "mlx-community/Llama-3-8B-Instruct-4bit"

	// Convert HuggingFace URL format to local directory name
	localName := strings.ReplaceAll(modelName, "/", "_")

	return filepath.Join(m.modelsDir, localName)
}

// ModelExists checks if a model is already cached locally
func (m *MLXModelManager) ModelExists(modelName string) bool {
	modelPath := m.GetModelPath(modelName)

	// Check for required MLX model files
	configPath := filepath.Join(modelPath, "config.json")
	if _, err := os.Stat(configPath); err != nil {
		return false
	}

	// Check for model weights (either safetensors or npz)
	safetensorsPath := filepath.Join(modelPath, "model.safetensors")
	weightsPath := filepath.Join(modelPath, "weights.npz")

	_, err1 := os.Stat(safetensorsPath)
	_, err2 := os.Stat(weightsPath)

	return err1 == nil || err2 == nil
}

// DeleteModel removes a model from local storage
func (m *MLXModelManager) DeleteModel(modelName string) error {
	modelPath := m.GetModelPath(modelName)
	return os.RemoveAll(modelPath)
}

// calculateDirSize calculates the total size of a directory
func (m *MLXModelManager) calculateDirSize(path string) (int64, error) {
	var size int64

	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// HuggingFaceModelInfo represents model information from HuggingFace API
type HuggingFaceModelInfo struct {
	ModelID     string   `json:"modelId"`
	Author      string   `json:"author"`
	Downloads   int      `json:"downloads"`
	Tags        []string `json:"tags"`
	LastUpdated string   `json:"lastModified"`
}

// SearchMLXModels searches HuggingFace for MLX models
func SearchMLXModels(query string, limit int) ([]HuggingFaceModelInfo, error) {
	// Search HuggingFace for models with MLX tag
	url := fmt.Sprintf("https://huggingface.co/api/models?search=%s&filter=mlx&limit=%d", query, limit)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to search HuggingFace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HuggingFace API returned status %d", resp.StatusCode)
	}

	var models []HuggingFaceModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return nil, fmt.Errorf("failed to parse HuggingFace response: %w", err)
	}

	return models, nil
}

func getMLXBaseURL(modelID string) string {
	if base := os.Getenv("OLLAMA_MLX_BASE_URL"); base != "" {
		return fmt.Sprintf("%s/%s", strings.TrimSuffix(base, "/"), modelID)
	}

	return fmt.Sprintf("https://huggingface.co/%s", modelID)
}

func shouldDownloadFile(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "config.json", "tokenizer.json", "tokenizer_config.json", "generation_config.json", "special_tokens_map.json", "tokenizer.model",
		"preprocessor_config.json", "chat_template.json", "added_tokens.json", "vocab.json", "merges.txt":
		// Core model files + vision model files + tokenizer files
		return true
	}

	if strings.HasSuffix(lower, ".npz") {
		return true
	}

	if strings.HasSuffix(lower, ".safetensors") || strings.HasSuffix(lower, ".safetensors.index.json") {
		return true
	}

	// Sharded weights: model-00001-of-000xx.safetensors
	if strings.HasPrefix(lower, "model-") && strings.Contains(lower, ".safetensors") {
		return true
	}

	// GGUF model files (e.g. from TheBloke or other GGUF repos on HuggingFace)
	if strings.HasSuffix(lower, ".gguf") {
		return true
	}

	return false
}

// computeDigest derives a stable digest from filenames and sizes to avoid
// hashing multi‑GB payloads. It is intentionally lightweight so it can run on
// large local caches without blocking.
func computeDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// include name + size so digest changes when any weight differs
		fmt.Fprintf(h, "%s:%d\n", rel, info.Size())
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// getHFToken reads the stored HuggingFace token from ~/.ollmlx/hf_token
func getHFToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tokenPath := filepath.Join(home, ".ollmlx", "hf_token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (m *MLXModelManager) fetchHFFileList(ctx context.Context, modelID string) ([]string, map[string]int64, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s", modelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}

	// Allow HF tokens from common env vars if provided.
	token := getHFToken()
	if token == "" {
		for _, key := range []string{"HUGGINGFACEHUB_API_TOKEN", "HUGGING_FACE_HUB_TOKEN", "HF_TOKEN"} {
			if tok := strings.TrimSpace(os.Getenv(key)); tok != "" {
				token = tok
				break
			}
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, nil, fmt.Errorf("huggingface api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var meta hfModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, nil, err
	}

	var files []string
	sizes := make(map[string]int64)
	for _, sib := range meta.Siblings {
		name := sib.RFilename
		if name == "" {
			continue
		}
		if !shouldDownloadFile(name) {
			continue
		}
		size := sib.Size
		if sib.LFS.Size > 0 {
			size = sib.LFS.Size
		}
		files = append(files, name)
		sizes[name] = size
	}

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no downloadable model files found for %s", modelID)
	}

	return files, sizes, nil
}

// MLXDownloadProgress represents download progress for a single file
type MLXDownloadProgress struct {
	Filename  string // Current filename being downloaded
	Completed int64  // Bytes completed for this file
	Total     int64  // Total bytes for this file
	Status    string // Status message
}

// DownloadMLXModel downloads an MLX model from HuggingFace
// The progress callback receives per-file progress so each file can be tracked separately
func (m *MLXModelManager) DownloadMLXModel(ctx context.Context, modelID string, progressFn func(MLXDownloadProgress)) error {
	modelPath := m.GetModelPath(modelID)

	// Create model directory
	if err := os.MkdirAll(modelPath, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(modelPath)
		}
	}()

	files, sizes, err := m.fetchHFFileList(ctx, modelID)
	if err != nil {
		// Fallback to a legacy file list so we still support minimal MLX model layouts.
		// Note: GGUF-only repos will fail here because neither safetensors nor weights.npz
		// exist. With a correctly populated HF repo, fetchHFFileList should succeed.
		slog.Warn("failed to fetch HuggingFace file list, falling back to defaults", "model", modelID, "error", err)
		files = []string{"config.json", "tokenizer.json", "tokenizer_config.json", "model.safetensors", "weights.npz"}
		sizes = map[string]int64{}
	}

	baseURL := fmt.Sprintf("%s/resolve/main", getMLXBaseURL(modelID))
	client := &http.Client{Timeout: 30 * time.Minute}

	for _, filename := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		fileURL := fmt.Sprintf("%s/%s", baseURL, filename)
		destPath := filepath.Join(modelPath, filename)
		expectedSize := sizes[filename]

		// Track if we've discovered the file size
		var fileTotal int64 = expectedSize

		// Only send initial progress if we already know the size
		// Otherwise wait until we learn it from Content-Length
		if progressFn != nil && fileTotal > 0 {
			progressFn(MLXDownloadProgress{
				Filename:  filename,
				Completed: 0,
				Total:     fileTotal,
				Status:    fmt.Sprintf("pulling %s", filename),
			})
		}

		_, err := m.downloadFileWithSize(ctx, client, fileURL, destPath, expectedSize, func(fileDownloaded int64, discoveredTotal int64) {
			// Update file total if we discovered it from Content-Length
			if fileTotal == 0 && discoveredTotal > 0 {
				slog.Debug("discovered file total", "filename", filename, "discoveredTotal", discoveredTotal)
				fileTotal = discoveredTotal
			}
			// Only report progress if we know the total size (for progress bar display)
			if progressFn != nil && fileTotal > 0 {
				progressFn(MLXDownloadProgress{
					Filename:  filename,
					Completed: fileDownloaded,
					Total:     fileTotal,
					Status:    fmt.Sprintf("pulling %s", filename),
				})
			}
		})

		if err != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("failed to download %s: %w", filename, err)
		}
	}

	if progressFn != nil {
		progressFn(MLXDownloadProgress{
			Status: "success",
		})
	}

	cleanup = false

	// Compute a lightweight digest for listing/show calls.
	if digest, err := computeDigest(modelPath); err == nil {
		if progressFn != nil {
			progressFn(MLXDownloadProgress{
				Status: fmt.Sprintf("digest %s", digest),
			})
		}
	}

	return nil
}

// downloadFile downloads a file from a URL to a local path with resume support
func (m *MLXModelManager) downloadFile(ctx context.Context, client *http.Client, url, destPath string, expectSize int64, progress func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Skip download if the target already exists with the expected size.
	// Note: We still need to count its size towards the total progress
	if stat, err := os.Stat(destPath); err == nil && expectSize > 0 && stat.Size() == expectSize {
		if progress != nil {
			progress(expectSize)
		}
		return nil
	}

	tmpPath := destPath + ".part"

	// Check for existing partial download to resume
	var existingSize int64
	if stat, err := os.Stat(tmpPath); err == nil {
		existingSize = stat.Size()
		// If the partial file is already the expected size, just rename it
		if expectSize > 0 && existingSize == expectSize {
			if progress != nil {
				progress(expectSize)
			}
			return os.Rename(tmpPath, destPath)
		}
		// If partial file is larger than expected, something is wrong - start fresh
		if expectSize > 0 && existingSize > expectSize {
			os.Remove(tmpPath)
			existingSize = 0
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	// Add HuggingFace token for authentication
	token := getHFToken()
	if token == "" {
		for _, key := range []string{"HUGGINGFACEHUB_API_TOKEN", "HUGGING_FACE_HUB_TOKEN", "HF_TOKEN"} {
			if tok := strings.TrimSpace(os.Getenv(key)); tok != "" {
				token = tok
				break
			}
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Request resume from existing position if we have a partial file
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("authentication required - please run 'ollmlx login' with your HuggingFace token")
	}

	// Handle response based on status code
	var out *os.File
	var resuming bool

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Server supports resume - append to existing file
		resuming = true
		out, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		// Report the already-downloaded bytes as progress
		if progress != nil {
			progress(existingSize)
		}
	case http.StatusOK:
		// Full download (server doesn't support Range, or no partial file)
		if existingSize > 0 {
			// Server didn't honor Range request, start fresh
			os.Remove(tmpPath)
		}
		out, err = os.Create(tmpPath)
		if err != nil {
			return err
		}
	case http.StatusRequestedRangeNotSatisfiable:
		// Range not satisfiable - file might be complete or corrupted
		io.Copy(io.Discard, resp.Body)
		os.Remove(tmpPath)
		// Retry without Range header by recursing with no partial file
		return m.downloadFile(ctx, client, url, destPath, expectSize, progress)
	default:
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	defer out.Close()

	// Create a proxy reader that reports progress
	reader := &ProgressReader{
		Reader: resp.Body,
		Callback: func(n int64) {
			if progress != nil {
				progress(n)
			}
		},
	}

	if _, err = io.Copy(out, reader); err != nil {
		// Don't remove partial file on error - allows resume on retry
		if !resuming {
			// Only keep partial for resume if we were starting fresh
			// and got some data. Check if file has content.
			if stat, statErr := os.Stat(tmpPath); statErr == nil && stat.Size() > 0 {
				// Keep the partial file for resume
				return fmt.Errorf("download interrupted (partial file saved for resume): %w", err)
			}
			os.Remove(tmpPath)
		}
		return err
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}

	return nil
}

// downloadFileWithSize downloads a file and returns the actual size downloaded
// progress callback receives (bytesDownloadedSoFar, totalFileSize)
func (m *MLXModelManager) downloadFileWithSize(ctx context.Context, client *http.Client, url, destPath string, expectSize int64, progress func(int64, int64)) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return 0, err
	}

	// Check if file already exists with expected size
	if stat, err := os.Stat(destPath); err == nil && expectSize > 0 && stat.Size() == expectSize {
		if progress != nil {
			progress(expectSize, expectSize)
		}
		return expectSize, nil
	}

	tmpPath := destPath + ".part"

	// Check for existing partial download to resume
	var existingSize int64
	if stat, err := os.Stat(tmpPath); err == nil {
		existingSize = stat.Size()
		// If partial file matches expected size, just rename it
		if expectSize > 0 && existingSize == expectSize {
			if progress != nil {
				progress(expectSize, expectSize)
			}
			return expectSize, os.Rename(tmpPath, destPath)
		}
		// If partial file is larger than expected, start fresh
		if expectSize > 0 && existingSize > expectSize {
			os.Remove(tmpPath)
			existingSize = 0
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	// Add HuggingFace token
	token := getHFToken()
	if token == "" {
		for _, key := range []string{"HUGGINGFACEHUB_API_TOKEN", "HUGGING_FACE_HUB_TOKEN", "HF_TOKEN"} {
			if tok := strings.TrimSpace(os.Getenv(key)); tok != "" {
				token = tok
				break
			}
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Request resume from existing position
	if existingSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingSize))
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		io.Copy(io.Discard, resp.Body)
		return 0, fmt.Errorf("authentication required - please run 'ollmlx login' with your HuggingFace token")
	}

	// Get actual file size from Content-Length or Content-Range
	var actualTotalSize int64
	if expectSize > 0 {
		actualTotalSize = expectSize
	} else if cl := resp.Header.Get("Content-Length"); cl != "" {
		if size, err := strconv.ParseInt(cl, 10, 64); err == nil {
			if existingSize > 0 && resp.StatusCode == http.StatusPartialContent {
				// Content-Length is remaining bytes, add existing
				actualTotalSize = existingSize + size
			} else {
				actualTotalSize = size
			}
		}
	}
	// DEBUG: Log Content-Length discovery
	slog.Debug("downloadFileWithSize", "url", url, "statusCode", resp.StatusCode, "contentLength", resp.Header.Get("Content-Length"), "actualTotalSize", actualTotalSize)
	// Try Content-Range header for partial content
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		// Format: bytes 100-999/1000
		if idx := strings.LastIndex(cr, "/"); idx >= 0 {
			if size, err := strconv.ParseInt(cr[idx+1:], 10, 64); err == nil && size > 0 {
				actualTotalSize = size
			}
		}
	}

	var out *os.File
	var resuming bool
	var downloaded int64

	switch resp.StatusCode {
	case http.StatusPartialContent:
		resuming = true
		out, err = os.OpenFile(tmpPath, os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return 0, err
		}
		downloaded = existingSize
		if progress != nil {
			progress(downloaded, actualTotalSize)
		}
	case http.StatusOK:
		if existingSize > 0 {
			os.Remove(tmpPath)
		}
		out, err = os.Create(tmpPath)
		if err != nil {
			return 0, err
		}
		// Send initial progress with discovered total size to initialize progress bar
		if progress != nil && actualTotalSize > 0 {
			progress(0, actualTotalSize)
		}
	case http.StatusRequestedRangeNotSatisfiable:
		io.Copy(io.Discard, resp.Body)
		os.Remove(tmpPath)
		return m.downloadFileWithSize(ctx, client, url, destPath, expectSize, progress)
	default:
		io.Copy(io.Discard, resp.Body)
		return 0, fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	defer out.Close()

	// Read with progress tracking
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return downloaded, writeErr
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, actualTotalSize)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			// Keep partial for resume
			if !resuming && downloaded > 0 {
				return downloaded, fmt.Errorf("download interrupted (partial file saved for resume): %w", readErr)
			}
			return downloaded, readErr
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return downloaded, err
	}

	return downloaded, nil
}

// ProgressReader wraps an io.Reader to report progress
type ProgressReader struct {
	io.Reader
	Callback func(int64)
}

func (r *ProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 && r.Callback != nil {
		r.Callback(int64(n))
	}
	return n, err
}

// GetPopularMLXModels returns a curated list of popular/recommended MLX models
func GetPopularMLXModels() []string {
	return []string{
		"mlx-community/Llama-3.2-3B-Instruct-4bit",
		"mlx-community/Llama-3.2-1B-Instruct-4bit",
		"mlx-community/Mistral-7B-Instruct-v0.3-4bit",
		"mlx-community/Qwen2.5-7B-Instruct-4bit",
		"mlx-community/SmolLM2-1.7B-Instruct-4bit",
		"mlx-community/Phi-3.5-mini-instruct-4bit",
		"mlx-community/gemma-2-2b-it-4bit",
	}
}
