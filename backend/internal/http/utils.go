package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"agr_3x_ui/internal/services/sshclient"
)

const maxKeyUploadSize = 2 << 20

type convertSSHKeyResponse struct {
	Format      string `json:"format"`
	PrivateKey  string `json:"privateKey"`
	Fingerprint string `json:"fingerprint"`
}

func (h *Handler) ConvertSSHKey(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		respondError(c, http.StatusBadRequest, "SSH_KEY_FILE", "file is required")
		return
	}
	if file.Size > maxKeyUploadSize {
		respondError(c, http.StatusBadRequest, "SSH_KEY_FILE", "file is too large")
		return
	}
	passphrase := strings.TrimSpace(c.PostForm("passphrase"))
	data, err := readFormFile(file)
	if err != nil {
		respondError(c, http.StatusBadRequest, "SSH_KEY_FILE", "failed to read file")
		return
	}

	keyBytes := data
	if isPPK(file.Filename, data) {
		keyBytes, err = convertPPK(c.Request.Context(), data, passphrase)
		if err != nil {
			respondError(c, http.StatusBadRequest, "SSH_KEY_CONVERT", err.Error())
			return
		}
	} else if !looksLikePrivateKey(data) {
		respondError(c, http.StatusBadRequest, "SSH_KEY_CONVERT", "unsupported key format")
		return
	}

	normalized, fingerprint, err := sshclient.NormalizePrivateKey(keyBytes, passphrase)
	if err != nil {
		if errors.Is(err, sshclient.ErrPassphraseRequired) {
			respondError(c, http.StatusBadRequest, "SSH_KEY_PASSPHRASE", "key is encrypted, passphrase required")
			return
		}
		respondError(c, http.StatusBadRequest, "SSH_KEY_CONVERT", "invalid ssh private key")
		return
	}
	log.Printf("ssh key converted fingerprint=%s filename=%s", fingerprint, file.Filename)
	c.JSON(http.StatusOK, convertSSHKeyResponse{
		Format:      "openssh",
		PrivateKey:  normalized,
		Fingerprint: fingerprint,
	})
}

func readFormFile(file *multipart.FileHeader) ([]byte, error) {
	handle, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	return io.ReadAll(handle)
}

func isPPK(filename string, data []byte) bool {
	if strings.HasSuffix(strings.ToLower(filename), ".ppk") {
		return true
	}
	return bytes.HasPrefix(data, []byte("PuTTY-User-Key-File-"))
}

func looksLikePrivateKey(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("-----BEGIN ")) && bytes.Contains(trimmed, []byte("PRIVATE KEY-----")) {
		return true
	}
	return bytes.HasPrefix(trimmed, []byte("-----BEGIN OPENSSH PRIVATE KEY-----"))
}

func convertPPK(ctx context.Context, data []byte, passphrase string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "ppk")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	inputPath := filepath.Join(dir, "input.ppk")
	outputPath := filepath.Join(dir, "output.key")
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "puttygen", inputPath, "-O", "private-openssh", "-o", outputPath)
	if passphrase != "" {
		cmd.Stdin = strings.NewReader(passphrase + "\n")
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.ToLower(string(output))
		if strings.Contains(msg, "passphrase") || strings.Contains(msg, "encrypted") {
			return nil, errors.New("ppk is encrypted, passphrase required")
		}
		return nil, errors.New("invalid ppk")
	}
	keyBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	return keyBytes, nil
}
