package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"agr_3x_ui/internal/agentfs"
)

type fsEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	Modified time.Time `json:"modified"`
	UID      int       `json:"uid,omitempty"`
	GID      int       `json:"gid,omitempty"`
}

type fsStatResponse struct {
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	Modified time.Time `json:"modified"`
	UID      int       `json:"uid,omitempty"`
	GID      int       `json:"gid,omitempty"`
}

type fsReadResponse struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

type fsWriteRequest struct {
	Content string  `json:"content"`
	Mode    *string `json:"mode"`
}

type fsPathRequest struct {
	Path string `json:"path"`
}

type fsRenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type fsDeleteRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func (s *state) fsListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	clean, err := agentfs.EnsureAllowed(raw, agentfs.OpList)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	real := agentfs.RealPath(clean)
	if agentfs.IsBlocked(real) {
		writeFSError(w, http.StatusForbidden, "forbidden", "path is forbidden")
		return
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	resp := make([]fsEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		itemPath := path.Join(clean, entry.Name())
		typ := "file"
		if info.IsDir() {
			typ = "dir"
		}
		uid, gid := fileOwner(info)
		resp = append(resp, fsEntry{
			Name:     entry.Name(),
			Path:     itemPath,
			Type:     typ,
			Size:     info.Size(),
			Mode:     fmt.Sprintf("%#o", info.Mode().Perm()),
			Modified: info.ModTime(),
			UID:      uid,
			GID:      gid,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": clean, "entries": resp})
}

func (s *state) fsStatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	clean, err := agentfs.EnsureAllowed(raw, agentfs.OpStat)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	real := agentfs.RealPath(clean)
	if agentfs.IsBlocked(real) {
		writeFSError(w, http.StatusForbidden, "forbidden", "path is forbidden")
		return
	}
	info, err := os.Stat(clean)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, "not_found", "path not found")
		return
	}
	typ := "file"
	if info.IsDir() {
		typ = "dir"
	}
	uid, gid := fileOwner(info)
	resp := fsStatResponse{
		Path:     clean,
		Type:     typ,
		Size:     info.Size(),
		Mode:     fmt.Sprintf("%#o", info.Mode().Perm()),
		Modified: info.ModTime(),
		UID:      uid,
		GID:      gid,
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": resp})
}

func (s *state) fsReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	clean, err := agentfs.EnsureAllowed(raw, agentfs.OpRead)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	real := agentfs.RealPath(clean)
	if agentfs.IsBlocked(real) {
		writeFSError(w, http.StatusForbidden, "forbidden", "path is forbidden")
		return
	}
	content, size, err := agentfs.ReadTextFile(clean, s.fsMaxTextBytes())
	if err != nil {
		code := mapFSError(err)
		if errors.Is(err, agentfs.ErrTooLarge) {
			writeFSError(w, http.StatusBadRequest, code, "file too large")
			return
		}
		if errors.Is(err, agentfs.ErrNotText) {
			writeFSError(w, http.StatusBadRequest, code, "file is not text")
			return
		}
		writeFSError(w, http.StatusBadRequest, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "data": fsReadResponse{Path: clean, Size: size, Content: content}})
}

func (s *state) fsWriteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req fsWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	clean, err := agentfs.EnsureParentAllowed(raw, agentfs.OpWrite)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	real := agentfs.RealPath(clean)
	if agentfs.IsBlocked(real) {
		writeFSError(w, http.StatusForbidden, "forbidden", "path is forbidden")
		return
	}
	maxBytes := s.fsMaxTextBytes()
	if maxBytes > 0 && int64(len(req.Content)) > maxBytes {
		writeFSError(w, http.StatusBadRequest, "too_large", "file too large")
		return
	}
	if !agentfs.DetectText([]byte(req.Content)) {
		writeFSError(w, http.StatusBadRequest, "not_text", "content is not text")
		return
	}
	perm := os.FileMode(0644)
	if req.Mode != nil {
		if val, err := strconv.ParseUint(strings.TrimSpace(*req.Mode), 8, 32); err == nil {
			perm = os.FileMode(val)
		}
	}
	if err := os.WriteFile(clean, []byte(req.Content), perm); err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *state) fsMkdirHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req fsPathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
		return
	}
	clean, err := agentfs.EnsureParentAllowed(req.Path, agentfs.OpMkdir)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *state) fsRenameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req fsRenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
		return
	}
	from, err := agentfs.EnsureAllowed(req.From, agentfs.OpRename)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	to, err := agentfs.EnsureParentAllowed(req.To, agentfs.OpRename)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	if err := os.Rename(from, to); err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *state) fsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var req fsDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
		return
	}
	clean, err := agentfs.EnsureParentAllowed(req.Path, agentfs.OpDelete)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	if req.Recursive {
		if err := os.RemoveAll(clean); err != nil {
			writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
			return
		}
	} else {
		if err := os.Remove(clean); err != nil {
			writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *state) fsUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	maxBytes := s.fsMaxUploadBytes()
	if maxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	rawTarget := strings.TrimSpace(r.URL.Query().Get("path"))
	reader, err := r.MultipartReader()
	if err != nil {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "invalid multipart payload")
		return
	}
	var partFound bool
	for {
		part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			writeFSError(w, http.StatusBadRequest, "invalid_payload", "failed to read multipart")
			return
		}
		if part.FormName() != "file" {
			continue
		}
		partFound = true
		filename := part.FileName()
		if filename == "" {
			writeFSError(w, http.StatusBadRequest, "invalid_payload", "file name required")
			return
		}
		target, err := agentfs.ResolveUploadTarget(rawTarget, filename)
		if err != nil {
			writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
			return
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
			return
		}
		written, err := io.Copy(out, agentfs.LimitReader(part, maxBytes))
		_ = out.Close()
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeFSError(w, http.StatusBadRequest, "too_large", "upload too large")
				return
			}
			writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": written, "path": target})
		return
	}
	if !partFound {
		writeFSError(w, http.StatusBadRequest, "invalid_payload", "file required")
		return
	}
}

func (s *state) fsDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeFSError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	clean, err := agentfs.EnsureAllowed(raw, agentfs.OpDownload)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, mapFSError(err), "invalid path")
		return
	}
	real := agentfs.RealPath(clean)
	if agentfs.IsBlocked(real) {
		writeFSError(w, http.StatusForbidden, "forbidden", "path is forbidden")
		return
	}
	file, err := os.Open(clean)
	if err != nil {
		writeFSError(w, http.StatusBadRequest, "not_found", "path not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	if info.IsDir() {
		writeFSError(w, http.StatusBadRequest, "invalid_file", "path is a directory")
		return
	}
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeFSError(w, http.StatusBadRequest, "io_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(buf[:n]))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(clean)))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (s *state) fsMaxTextBytes() int64 {
	if s == nil {
		return 0
	}
	if s.cfg.FSMaxTextBytes > 0 {
		return s.cfg.FSMaxTextBytes
	}
	return 2 * 1024 * 1024
}

func (s *state) fsMaxUploadBytes() int64 {
	if s == nil {
		return 0
	}
	if s.cfg.FSMaxUploadBytes > 0 {
		return s.cfg.FSMaxUploadBytes
	}
	return 50 * 1024 * 1024
}

func mapFSError(err error) string {
	switch {
	case errors.Is(err, agentfs.ErrForbiddenPath):
		return "forbidden"
	case errors.Is(err, agentfs.ErrInvalidPath):
		return "invalid_path"
	case errors.Is(err, agentfs.ErrNotText):
		return "not_text"
	case errors.Is(err, agentfs.ErrTooLarge):
		return "too_large"
	default:
		return "io_error"
	}
}

func writeFSError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}
