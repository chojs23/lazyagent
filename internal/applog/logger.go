package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const fileName = "lazyagent.log"

type Logger struct {
	path string
}

var (
	defaultMu     sync.RWMutex
	defaultLogger = &Logger{}
)

func SetDefault(l *Logger) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if l == nil {
		defaultLogger = &Logger{}
		return
	}
	defaultLogger = l
}

func Default() *Logger {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultLogger
}

func NewDefault() (*Logger, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return &Logger{path: path}, nil
}

func NewForPath(path string) *Logger {
	return &Logger{path: path}
}

func DefaultPath() (string, error) {
	dataDir, err := defaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, fileName), nil
}

func defaultDataDir() (string, error) {
	if dbPath := os.Getenv("LAZYAGENT_DB_PATH"); dbPath != "" {
		return filepath.Dir(dbPath), nil
	}
	if dataDir := os.Getenv("LAZYAGENT_DATA_DIR"); dataDir != "" {
		return dataDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home for logger: %w", err)
	}
	return filepath.Join(home, ".lazyagent"), nil
}

func Error(context string, err error) {
	Default().Error(context, err)
}

func Panic(context string, value any) string {
	return Default().Panic(context, value)
}

func (l *Logger) Error(context string, err error) {
	if err == nil {
		return
	}
	l.write("ERROR", context, err.Error())
}

func (l *Logger) Panic(context string, value any) string {
	stack := strings.TrimRight(string(debug.Stack()), "\n")
	message := fmt.Sprintf("panic: %v", value)
	l.write("PANIC", context, message, stack)
	if stack == "" {
		return message
	}
	return message + "\n" + stack
}

func (l *Logger) write(level, context string, lines ...string) {
	if l == nil {
		return
	}
	path := l.path
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	ts := time.Now().Format(time.RFC3339)
	header := fmt.Sprintf("%s [%s]", ts, level)
	if context != "" {
		header += " " + context
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	for _, line := range lines {
		for _, part := range strings.Split(strings.TrimRight(line, "\n"), "\n") {
			if part == "" {
				b.WriteString("  \n")
				continue
			}
			b.WriteString("  ")
			b.WriteString(part)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	_, _ = f.WriteString(b.String())
}
