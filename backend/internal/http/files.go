package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"agr_3x_ui/internal/db"
)

const (
	fileTypeDir  = "dir"
	fileTypeFile = "file"
)

var errPathNotAllowed = errors.New("path is not allowed")

type fileRoot struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Source string `json:"source"`
}

type fileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Type      string    `json:"type"`
	MimeGuess string    `json:"mime_guess"`
}

type fileReadResponse struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Offset int64  `json:"offset"`
	Limit  int64  `json:"limit"`
	Data   string `json:"data"`
}

type filePathRequest struct {
	Path string `json:"path"`
}

type fileRenameRequest struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

func (h *Handler) ListFileRoots(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	roots, source := h.allowedRootsForNode(node)
	out := make([]fileRoot, 0, len(roots))
	for _, root := range roots {
		out = append(out, fileRoot{Path: root, Label: root, Source: source})
	}
	respondStatus(c, http.StatusOK, gin.H{"roots": out})
}

func (h *Handler) ListFiles(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	reqPath := strings.TrimSpace(c.Query("path"))
	roots, _ := h.allowedRootsForNode(node)
	if reqPath == "" && len(roots) > 0 {
		reqPath = roots[0]
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, _, err := h.resolveExistingPath(c, sftpClient, node, reqPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	entries, err := sftpClient.ReadDir(cleanPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "LIST_FAILED", "failed to list directory")
		return
	}
	resp := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		itemPath := path.Join(cleanPath, entry.Name())
		fileType := fileTypeFile
		if entry.IsDir() {
			fileType = fileTypeDir
		}
		mimeGuess := ""
		if !entry.IsDir() {
			mimeGuess = mime.TypeByExtension(path.Ext(entry.Name()))
		}
		resp = append(resp, fileEntry{
			Name:      entry.Name(),
			Path:      itemPath,
			IsDir:     entry.IsDir(),
			Size:      entry.Size(),
			Modified:  entry.ModTime(),
			Type:      fileType,
			MimeGuess: mimeGuess,
		})
	}
	h.auditEvent(c, &node.ID, "FILE_LIST", "ok", nil, gin.H{"path": cleanPath}, nil)
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) ReadFileChunk(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	reqPath := strings.TrimSpace(c.Query("path"))
	offset := parseInt64Query(c, "offset", 0)
	maxPreview := h.previewMaxBytes()
	limit := parseInt64Query(c, "limit", maxPreview)
	if limit <= 0 {
		limit = maxPreview
	}
	if limit > maxPreview {
		respondError(c, http.StatusBadRequest, "LIMIT_TOO_LARGE", "limit too large")
		return
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, _, err := h.resolveExistingPath(c, sftpClient, node, reqPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	file, err := sftpClient.Open(cleanPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to open file")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to stat file")
		return
	}
	if info.IsDir() {
		respondError(c, http.StatusBadRequest, "INVALID_FILE", "path is a directory")
		return
	}
	if info.Size() > maxPreview {
		respondError(c, http.StatusBadRequest, "FILE_TOO_LARGE", "file too large for preview")
		return
	}
	if offset < 0 {
		offset = 0
	}
	if offset > info.Size() {
		offset = info.Size()
	}
	if limit > info.Size()-offset {
		limit = info.Size() - offset
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to seek file")
		return
	}
	buf := make([]byte, limit)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to read file")
		return
	}
	data := string(buf[:n])
	h.auditEvent(c, &node.ID, "FILE_READ", "ok", nil, gin.H{"path": cleanPath, "bytes": n}, nil)
	respondStatus(c, http.StatusOK, fileReadResponse{
		Path:   cleanPath,
		Size:   info.Size(),
		Offset: offset,
		Limit:  int64(n),
		Data:   data,
	})
}

func (h *Handler) TailFile(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	reqPath := strings.TrimSpace(c.Query("path"))
	maxTail := h.tailMaxBytes()
	bytesLimit := parseInt64Query(c, "bytes", maxTail)
	if bytesLimit <= 0 {
		bytesLimit = maxTail
	}
	if bytesLimit > maxTail {
		bytesLimit = maxTail
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, _, err := h.resolveExistingPath(c, sftpClient, node, reqPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	file, err := sftpClient.Open(cleanPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to open file")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to stat file")
		return
	}
	if info.IsDir() {
		respondError(c, http.StatusBadRequest, "INVALID_FILE", "path is a directory")
		return
	}
	size := info.Size()
	start := size - bytesLimit
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to seek file")
		return
	}
	buf := make([]byte, size-start)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		respondError(c, http.StatusBadRequest, "READ_FAILED", "failed to read file")
		return
	}
	data := string(buf[:n])
	h.auditEvent(c, &node.ID, "FILE_TAIL", "ok", nil, gin.H{"path": cleanPath, "bytes": n}, nil)
	respondStatus(c, http.StatusOK, fileReadResponse{
		Path:   cleanPath,
		Size:   info.Size(),
		Offset: start,
		Limit:  int64(n),
		Data:   data,
	})
}

func (h *Handler) DownloadFile(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	reqPath := strings.TrimSpace(c.Query("path"))
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, _, err := h.resolveExistingPath(c, sftpClient, node, reqPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	file, err := sftpClient.Open(cleanPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "DOWNLOAD_FAILED", "failed to open file")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		respondError(c, http.StatusBadRequest, "DOWNLOAD_FAILED", "failed to stat file")
		return
	}
	if info.IsDir() {
		respondError(c, http.StatusBadRequest, "INVALID_FILE", "path is a directory")
		return
	}
	name := path.Base(cleanPath)
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	written, err := io.Copy(c.Writer, file)
	if err != nil {
		h.auditEvent(c, &node.ID, "FILE_DOWNLOAD", "error", nil, gin.H{"path": cleanPath, "bytes": written}, errString(err))
		return
	}
	h.auditEvent(c, &node.ID, "FILE_DOWNLOAD", "ok", nil, gin.H{"path": cleanPath, "bytes": written}, nil)
}

func (h *Handler) UploadFile(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	reqPath := strings.TrimSpace(c.Query("path"))
	if reqPath == "" {
		respondError(c, http.StatusBadRequest, "INVALID_PATH", "path required")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "file required")
		return
	}
	src, err := file.Open()
	if err != nil {
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "failed to open upload")
		return
	}
	defer src.Close()

	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	uploadDir, err := normalizePath(reqPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	cleanPath, err := h.resolveParentAllowed(c, sftpClient, node, path.Join(uploadDir, path.Base(file.Filename)))
	if err != nil {
		handlePathError(c, err)
		return
	}
	dirInfo, err := sftpClient.Stat(uploadDir)
	if err != nil || !dirInfo.IsDir() {
		respondError(c, http.StatusBadRequest, "INVALID_PATH", "target directory not found")
		return
	}
	dst, err := sftpClient.OpenFile(cleanPath, osCreateTruncFlags())
	if err != nil {
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "failed to open target")
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, src)
	if err != nil {
		h.auditEvent(c, &node.ID, "FILE_UPLOAD", "error", nil, gin.H{"path": cleanPath, "bytes": written}, errString(err))
		respondError(c, http.StatusBadRequest, "UPLOAD_FAILED", "failed to upload")
		return
	}
	h.auditEvent(c, &node.ID, "FILE_UPLOAD", "ok", nil, gin.H{"path": cleanPath, "bytes": written}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) Mkdir(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req filePathRequest
	if !parseJSONBody(c, &req) {
		return
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, err := h.resolveParentAllowed(c, sftpClient, node, req.Path)
	if err != nil {
		handlePathError(c, err)
		return
	}
	if err := sftpClient.Mkdir(cleanPath); err != nil {
		respondError(c, http.StatusBadRequest, "MKDIR_FAILED", "failed to create directory")
		return
	}
	h.auditEvent(c, &node.ID, "FILE_MKDIR", "ok", nil, gin.H{"path": cleanPath}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) RenamePath(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req fileRenameRequest
	if !parseJSONBody(c, &req) {
		return
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	oldPath, _, err := h.resolveExistingPath(c, sftpClient, node, req.OldPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	newPath, err := h.resolveParentAllowed(c, sftpClient, node, req.NewPath)
	if err != nil {
		handlePathError(c, err)
		return
	}
	if err := sftpClient.Rename(oldPath, newPath); err != nil {
		respondError(c, http.StatusBadRequest, "RENAME_FAILED", "failed to rename")
		return
	}
	h.auditEvent(c, &node.ID, "FILE_RENAME", "ok", nil, gin.H{"old": oldPath, "new": newPath}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) DeletePath(c *gin.Context) {
	node, err := h.getNode(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req filePathRequest
	if !parseJSONBody(c, &req) {
		return
	}
	sftpClient, closer, err := h.openSFTP(c.Request.Context(), node)
	if err != nil {
		respondError(c, http.StatusBadGateway, "SFTP_CONNECT", "failed to connect")
		return
	}
	defer closer()

	cleanPath, _, err := h.resolveExistingPath(c, sftpClient, node, req.Path)
	if err != nil {
		handlePathError(c, err)
		return
	}
	info, err := sftpClient.Stat(cleanPath)
	if err != nil {
		respondError(c, http.StatusBadRequest, "DELETE_FAILED", "failed to stat path")
		return
	}
	if info.IsDir() {
		if err := sftpClient.RemoveDirectory(cleanPath); err != nil {
			respondError(c, http.StatusBadRequest, "DELETE_FAILED", "failed to remove directory")
			return
		}
	} else {
		if err := sftpClient.Remove(cleanPath); err != nil {
			respondError(c, http.StatusBadRequest, "DELETE_FAILED", "failed to remove file")
			return
		}
	}
	h.auditEvent(c, &node.ID, "FILE_DELETE", "ok", nil, gin.H{"path": cleanPath}, nil)
	respondStatus(c, http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) openSFTP(ctx context.Context, node *db.Node) (*sftp.Client, func(), error) {
	if h == nil || h.Encryptor == nil {
		return nil, nil, errors.New("encryptor missing")
	}
	if node == nil {
		return nil, nil, errors.New("node missing")
	}
	if !node.SSHEnabled {
		return nil, nil, errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return nil, nil, errors.New("unsupported ssh auth method")
	}
	key, err := h.decryptSSHKey(node)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return nil, nil, errors.New("ssh config missing")
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            node.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	dialer := net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	sshClient := ssh.NewClient(sshConn, chans, reqs)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, err
	}
	cleanup := func() {
		_ = sftpClient.Close()
		_ = sshClient.Close()
	}
	return sftpClient, cleanup, nil
}

func (h *Handler) allowedRootsForNode(node *db.Node) ([]string, string) {
	if node != nil && len(node.AllowedRoots) > 0 {
		roots := sanitizeRoots([]string(node.AllowedRoots))
		if len(roots) > 0 {
			return roots, "node"
		}
	}
	return sanitizeRoots(h.FileAllowedRoots), "default"
}

func (h *Handler) resolveExistingPath(c *gin.Context, client *sftp.Client, node *db.Node, raw string) (string, string, error) {
	cleanPath, err := normalizePath(raw)
	if err != nil {
		return "", "", err
	}
	realPath, err := client.RealPath(cleanPath)
	if err != nil {
		return "", "", err
	}
	if !isPathAllowed(realPath, h.allowedRootsList(node)) {
		return "", "", errPathNotAllowed
	}
	return cleanPath, realPath, nil
}

func (h *Handler) resolveParentAllowed(c *gin.Context, client *sftp.Client, node *db.Node, raw string) (string, error) {
	cleanPath, err := normalizePath(raw)
	if err != nil {
		return "", err
	}
	base := path.Base(cleanPath)
	if base == "" || base == "." || base == ".." {
		return "", errors.New("invalid path")
	}
	parent := path.Dir(cleanPath)
	realParent, err := client.RealPath(parent)
	if err != nil {
		return "", err
	}
	if !isPathAllowed(realParent, h.allowedRootsList(node)) {
		return "", errPathNotAllowed
	}
	return cleanPath, nil
}

func (h *Handler) allowedRootsList(node *db.Node) []string {
	roots, _ := h.allowedRootsForNode(node)
	return roots
}

func normalizePath(raw string) (string, error) {
	clean := path.Clean(strings.TrimSpace(raw))
	if clean == "" || clean == "." {
		return "", errors.New("path required")
	}
	if !strings.HasPrefix(clean, "/") {
		return "", errors.New("path must be absolute")
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", errors.New("path traversal not allowed")
		}
	}
	return clean, nil
}

func sanitizeRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		item := strings.TrimSpace(root)
		if item == "" {
			continue
		}
		clean := path.Clean(item)
		if !strings.HasPrefix(clean, "/") {
			continue
		}
		if strings.Contains(clean, "..") {
			continue
		}
		if clean != "/" && strings.HasSuffix(clean, "/") {
			clean = strings.TrimSuffix(clean, "/")
		}
		out = append(out, clean)
	}
	return out
}

func isPathAllowed(target string, roots []string) bool {
	targetClean := path.Clean(target)
	for _, root := range roots {
		if root == "/" {
			return true
		}
		if strings.Contains(root, "*") {
			if matchRootPattern(targetClean, root) {
				return true
			}
			continue
		}
		if targetClean == root {
			return true
		}
		if strings.HasPrefix(targetClean, root+"/") {
			return true
		}
	}
	return false
}

func matchRootPattern(target string, pattern string) bool {
	targetSegs := splitPathSegments(target)
	patternSegs := splitPathSegments(pattern)
	if len(targetSegs) < len(patternSegs) {
		return false
	}
	for i, seg := range patternSegs {
		if seg == "*" {
			continue
		}
		if seg != targetSegs[i] {
			return false
		}
	}
	return true
}

func splitPathSegments(p string) []string {
	trimmed := strings.TrimPrefix(p, "/")
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Split(trimmed, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func parseInt64Query(c *gin.Context, key string, fallback int64) int64 {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return val
}

func osCreateTruncFlags() int {
	return os.O_WRONLY | os.O_CREATE | os.O_TRUNC
}

func handlePathError(c *gin.Context, err error) {
	if errors.Is(err, errPathNotAllowed) {
		respondError(c, http.StatusForbidden, "PATH_NOT_ALLOWED", "path is not allowed")
		return
	}
	respondError(c, http.StatusBadRequest, "INVALID_PATH", err.Error())
}

func (h *Handler) previewMaxBytes() int64 {
	if h != nil && h.FilePreviewMaxBytes > 0 {
		return h.FilePreviewMaxBytes
	}
	return 2 * 1024 * 1024
}

func (h *Handler) tailMaxBytes() int64 {
	if h != nil && h.FileTailMaxBytes > 0 {
		return h.FileTailMaxBytes
	}
	return 128 * 1024
}
