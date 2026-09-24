package clips

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

// DefaultService is nil when clips are disabled
var DefaultService *Service

// Setup enables clips when CLIP_STORAGE_PATH or CLIP_S3_BUCKET is set
func Setup() {
	var (
		storage Storage
		err     error
	)

	switch {
	case os.Getenv(environment.ClipS3Bucket) != "":
		storage, err = NewS3Storage(S3Config{
			Endpoint:  envOr(environment.ClipS3Endpoint, "s3.amazonaws.com"),
			Bucket:    os.Getenv(environment.ClipS3Bucket),
			AccessKey: os.Getenv(environment.ClipS3AccessKey),
			SecretKey: os.Getenv(environment.ClipS3SecretKey),
			Region:    os.Getenv(environment.ClipS3Region),
			Prefix:    os.Getenv(environment.ClipS3Prefix),
			UseSSL:    !strings.EqualFold(os.Getenv(environment.ClipS3UseSSL), "false"),
		})
	case os.Getenv(environment.ClipStoragePath) != "":
		storage, err = NewLocalStorage(os.Getenv(environment.ClipStoragePath))
	default:
		slog.Info("Clips: disabled, set CLIP_STORAGE_PATH or CLIP_S3_BUCKET to enable")
		return
	}
	if err != nil {
		slog.Error("Clips: storage setup failed, clips disabled", "err", err)
		return
	}

	config := Config{
		BufferDuration:  envDuration(environment.ClipBufferDuration),
		MaxClipDuration: envDuration(environment.ClipMaxDuration),
		DraftDirectory:  envOr(environment.ClipDraftPath, filepath.Join(os.TempDir(), "broadcast-box-clip-drafts")),
	}
	config.MaxDrafts, _ = strconv.Atoi(os.Getenv(environment.ClipMaxDrafts))

	DefaultService, err = NewService(storage, config)
	if err != nil {
		slog.Error("Clips: setup failed, clips disabled", "err", err)
		return
	}

	config = DefaultService.Config()
	slog.Info("Clips: enabled",
		"storage", storage.Name(),
		"bufferDuration", config.BufferDuration,
		"maxClipDuration", config.MaxClipDuration,
		"maxDrafts", config.MaxDrafts)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return 0
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		slog.Error("Clips: invalid duration, using default", "variable", key, "value", value)
		return 0
	}
	return duration
}
