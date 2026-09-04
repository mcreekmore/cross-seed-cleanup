package cleanup

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/mcreekmore/cross-seed-cleanup/internal/env"
)

var out io.Writer = os.Stdout

func SetupLogging() {
	var openErr error
	var openPath string
	if path := env.Getenv("LOG_FILE", ""); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			openErr, openPath = err, path // report it once the logger exists
		} else {
			out = io.MultiWriter(os.Stdout, f) // report + logs fan out to both
		}
	}

	level := parseLogLevel(env.Getenv("LOG_LEVEL", "info"))
	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	if openErr != nil {
		slog.Error("could not open LOG_FILE, logging to stdout only", "path", openPath, "err", openErr)
	}
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
