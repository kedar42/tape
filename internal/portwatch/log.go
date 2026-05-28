package portwatch

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type Logger struct {
	mu   sync.Mutex
	out  io.Writer
	name string
}

func NewLogger(out io.Writer, name string) *Logger {
	if out == nil {
		out = io.Discard
	}
	return &Logger{out: out, name: name}
}

func (l *Logger) Log(action string, fields map[string]any) {
	if l == nil {
		return
	}

	parts := []string{"name=" + logValue(l.name), "action=" + logValue(action)}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key == "name" || key == "action" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+logValue(fields[key]))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.out, strings.Join(parts, " "))
}

func logValue(value any) string {
	text := fmt.Sprint(value)
	if text == "" {
		return `""`
	}
	return strings.NewReplacer("\n", "\\n", "\r", "\\r", "\t", "\\t", " ", "_").Replace(text)
}
