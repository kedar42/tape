package portwatch

import (
	"context"
	"io"
	"log/slog"
)

type Logger struct {
	logger *slog.Logger
}

func NewLogger(out io.Writer) *Logger {
	if out == nil {
		out = io.Discard
	}
	return &Logger{logger: slog.New(slog.NewJSONHandler(out, nil))}
}

func (l *Logger) Log(action string, fields map[string]any) {
	if l == nil || l.logger == nil {
		return
	}

	attrs := make([]slog.Attr, 0, len(fields)+1)
	attrs = append(attrs, slog.String("action", action))
	for key, value := range fields {
		if key == "action" {
			continue
		}
		attrs = append(attrs, slog.Any(key, value))
	}
	l.logger.LogAttrs(context.Background(), slog.LevelInfo, "", attrs...)
}
