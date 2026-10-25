// Package logger creates the application's structured logger.
package logger

import (
	"io"
	"log/slog"
	"os"
)

func New(dst io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(dst, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func NewDefault() *slog.Logger {
	return New(os.Stdout)
}
