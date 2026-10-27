package logging

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxLogSize = 5 * 1024 * 1024

type Logger struct {
	mu   sync.Mutex
	dir  string
	file *os.File
	path string
}

type entry struct {
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Event   string         `json:"event,omitempty"`
	Message string         `json:"message,omitempty"`
	Task    string         `json:"task,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(gh[pousr]_[A-Za-z0-9_]{20,})`),
	regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]{20,})`),
	regexp.MustCompile(`(?i)(AGE-SECRET-KEY-1[A-Z0-9]+)`),
	regexp.MustCompile(`(?i)("?(token|password|secret|private_key)"?\s*[:=]\s*")([^"]+)(")`),
}

func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	logger := &Logger{dir: dir, path: filepath.Join(dir, "dsh-backup.log")}
	if err := logger.open(); err != nil {
		return nil, err
	}
	return logger, nil
}

func (l *Logger) open() error {
	if info, err := os.Stat(l.path); err == nil && info.Size() > maxLogSize {
		_ = l.file.Close()
		backup := l.path + ".1"
		_ = os.Remove(backup)
		_ = os.Rename(l.path, backup)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file = file
	return nil
}

func (l *Logger) Info(event, message string, fields map[string]any) {
	l.write("info", event, message, "", fields)
}

func (l *Logger) Warn(event, message string, fields map[string]any) {
	l.write("warn", event, message, "", fields)
}

func (l *Logger) Error(event, message string, fields map[string]any) {
	l.write("error", event, message, "", fields)
}

func (l *Logger) Task(level, taskID, event, message string, fields map[string]any) {
	l.write(level, event, message, taskID, fields)
}

func (l *Logger) write(level, event, message, taskID string, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if info, err := l.file.Stat(); err == nil && info.Size() > maxLogSize {
		_ = l.open()
	}
	for key, value := range fields {
		if text, ok := value.(string); ok {
			fields[key] = Redact(text)
		}
	}
	data := entry{
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Event:   event,
		Message: Redact(message),
		Task:    taskID,
		Fields:  fields,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = l.file.Write(append(raw, '\n'))
}

func Redact(value string) string {
	result := value
	for i, pattern := range secretPatterns {
		if i == 3 {
			result = pattern.ReplaceAllString(result, `${1}<redacted>${4}`)
		} else {
			result = pattern.ReplaceAllString(result, `<redacted>`)
		}
	}
	return result
}

func (l *Logger) Path() string {
	return l.path
}

func (l *Logger) Dir() string {
	return l.dir
}

func (l *Logger) Tail(maxLines int) string {
	raw, err := os.ReadFile(l.path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func (l *Logger) Export(destDir string, extras map[string][]byte) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(destDir, "dsh-backup-diagnostics-"+time.Now().Format("20060102-150405")+".zip")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	addFile := func(name string, data []byte) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.Modified = time.Now()
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = entry.Write(data)
		return err
	}
	if raw, err := os.ReadFile(l.path); err == nil {
		_ = addFile("dsh-backup.log", []byte(Redact(string(raw))))
	}
	if info, err := os.Stat(l.path + ".1"); err == nil && info.Size() < 20*1024*1024 {
		if raw, err := os.ReadFile(l.path + ".1"); err == nil {
			_ = addFile("dsh-backup.log.1", []byte(Redact(string(raw))))
		}
	}
	keys := make([]string, 0, len(extras))
	for key := range extras {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := addFile(key, extras[key]); err != nil {
			_ = writer.Close()
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}
