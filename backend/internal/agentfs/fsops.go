package agentfs

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidPath   = errors.New("invalid_path")
	ErrForbiddenPath = errors.New("forbidden")
	ErrNotText       = errors.New("not_text")
	ErrTooLarge      = errors.New("too_large")
)

type Config struct {
	MaxTextBytes   int64
	MaxUploadBytes int64
}

type Operation string

const (
	OpList     Operation = "list"
	OpStat     Operation = "stat"
	OpRead     Operation = "read"
	OpWrite    Operation = "write"
	OpMkdir    Operation = "mkdir"
	OpRename   Operation = "rename"
	OpDelete   Operation = "delete"
	OpUpload   Operation = "upload"
	OpDownload Operation = "download"
)

var blockedPrefixes = []string{"/proc", "/sys", "/dev"}

func NormalizePath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ErrInvalidPath
	}
	if strings.ContainsRune(value, '\x00') {
		return "", ErrInvalidPath
	}
	if strings.Contains(value, "\\") {
		return "", ErrInvalidPath
	}
	if !strings.HasPrefix(value, "/") {
		return "", ErrInvalidPath
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == ".." {
			return "", ErrInvalidPath
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == "" {
		return "", ErrInvalidPath
	}
	if !strings.HasPrefix(clean, "/") {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func IsBlocked(p string) bool {
	clean := path.Clean(p)
	for _, prefix := range blockedPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func EnsureAllowed(raw string, op Operation) (string, error) {
	clean, err := NormalizePath(raw)
	if err != nil {
		return "", err
	}
	if IsBlocked(clean) {
		return "", ErrForbiddenPath
	}
	return clean, nil
}

func EnsureParentAllowed(raw string, op Operation) (string, error) {
	clean, err := EnsureAllowed(raw, op)
	if err != nil {
		return "", err
	}
	parent := path.Dir(clean)
	if parent == "." || parent == "" {
		parent = "/"
	}
	if IsBlocked(parent) {
		return "", ErrForbiddenPath
	}
	return clean, nil
}

func DetectText(sample []byte) bool {
	if len(sample) == 0 {
		return true
	}
	if !utf8.Valid(sample) {
		return false
	}
	ctype := http.DetectContentType(sample)
	if strings.HasPrefix(ctype, "text/") {
		return true
	}
	switch ctype {
	case "application/json", "application/x-yaml", "application/xml":
		return true
	}
	return false
}

func ReadTextFile(path string, maxBytes int64) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("is_dir")
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return "", info.Size(), ErrTooLarge
	}
	sniff := make([]byte, 4096)
	n, _ := file.Read(sniff)
	if !DetectText(sniff[:n]) {
		return "", info.Size(), ErrNotText
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", info.Size(), err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return "", info.Size(), err
	}
	return string(data), info.Size(), nil
}

func ResolveUploadTarget(target string, filename string) (string, error) {
	clean, err := EnsureParentAllowed(target, OpUpload)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(target, "/") {
		return path.Join(clean, path.Base(filename)), nil
	}
	info, err := os.Stat(clean)
	if err == nil && info.IsDir() {
		return path.Join(clean, path.Base(filename)), nil
	}
	return clean, nil
}

func LimitReader(src io.Reader, max int64) io.Reader {
	if max <= 0 {
		return src
	}
	return io.LimitReader(src, max)
}

func RealPath(p string) string {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return real
}
