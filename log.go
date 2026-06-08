package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

var out io.Writer = os.Stdout

func setupLogging() {
	level := parseLogLevel(getenv("LOG_LEVEL", "info"))
	handler := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
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
