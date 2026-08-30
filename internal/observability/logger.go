package observability

import (
	"io"
	"log/slog"
	"strings"
)

// NewLogger returns a structured logger whose format and level are configured
// without ever inspecting or logging environment variables.
func NewLogger(output io.Writer, format, level string) *slog.Logger {
	options := &slog.HandlerOptions{Level: parseLevel(level)}
	if strings.EqualFold(format, "json") {
		return slog.New(slog.NewJSONHandler(output, options))
	}
	return slog.New(slog.NewTextHandler(output, options))
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
