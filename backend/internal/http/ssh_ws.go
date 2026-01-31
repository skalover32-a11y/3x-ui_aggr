package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"agr_3x_ui/internal/http/middleware"
	"agr_3x_ui/internal/services/sshws"
)

const (
	wsReadLimit   = 64 * 1024
	wsWriteBuffer = 32 * 1024
)

type sshResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *Handler) SSHWebsocket(c *gin.Context) {
	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(wsReadLimit)

	writeClose := func(reason string) {
		reason = sanitizeReason(reason)
		if reason != "" {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","message":%q}`, reason)))
		}
		_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason))
		_ = ws.Close()
	}

	actor, role, err := h.authenticateWS(c)
	if err != nil {
		log.Printf("ssh ws auth failed: %v", err)
		writeClose("unauthorized")
		return
	}
	if !canSSHRole(role) {
		log.Printf("ssh ws forbidden user=%s role=%s", actor, role)
		writeClose("forbidden")
		return
	}
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		log.Printf("ssh ws node lookup failed user=%s error=%v", actor, err)
		writeClose("node not found")
		return
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		log.Printf("ssh ws missing config node=%s user=%s", node.ID, actor)
		writeClose("ssh config missing")
		return
	}
	if strings.EqualFold(strings.TrimSpace(node.SSHUser), "root") {
		log.Printf("ssh ws root blocked node=%s user=%s", node.ID, actor)
		writeClose("root login not allowed")
		return
	}
	if h.SSHManager == nil {
		h.SSHManager = sshws.NewManager(10)
	}
	release, err := h.SSHManager.TryAcquire()
	if err != nil {
		if errors.Is(err, sshws.ErrLimitReached) {
			log.Printf("ssh ws limit reached user=%s", actor)
			writeClose("ssh session limit reached")
			return
		}
		log.Printf("ssh ws limit error user=%s error=%v", actor, err)
		writeClose("ssh session limit error")
		return
	}
	defer release()

	sshKey, err := h.decryptSSHKey(node)
	if err != nil {
		log.Printf("ssh ws decrypt key failed node=%s user=%s error=%v", node.ID, actor, err)
		writeClose("failed to decrypt ssh key")
		return
	}
	client, session, stdin, stdout, stderr, err := h.openSSHShell(node.SSHHost, node.SSHPort, node.SSHUser, sshKey)
	if err != nil {
		log.Printf("ssh ws open failed node=%s user=%s error=%v", node.ID, actor, err)
		writeClose(fmt.Sprintf("ssh failed: %v", err))
		return
	}
	defer session.Close()
	defer client.Close()

	h.auditEvent(c, &node.ID, "SSH_OPEN", "ok", nil, gin.H{}, nil)
	log.Printf("ssh ws open node=%s user=%s", node.ID, actor)
	defer func() {
		h.auditEvent(c, &node.ID, "SSH_CLOSE", "ok", nil, gin.H{}, nil)
		log.Printf("ssh ws close node=%s user=%s", node.ID, actor)
	}()

	var writeMu sync.Mutex
	writeWS := func(messageType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return ws.WriteMessage(messageType, payload)
	}

	closeOnce := sync.Once{}
	closeAll := func(reason string) {
		closeOnce.Do(func() {
			_ = writeWS(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason))
			_ = ws.Close()
			_ = session.Close()
			_ = client.Close()
		})
	}

	idleTimeout := h.SSHIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 10 * time.Minute
	}
	idle := sshws.NewIdleTimer(idleTimeout, func() {
		closeAll("idle timeout")
	})
	defer idle.Stop()

	go func() {
		buf := make([]byte, wsWriteBuffer)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				_ = writeWS(websocket.TextMessage, buf[:n])
			}
			if readErr != nil {
				closeAll("ssh closed")
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, wsWriteBuffer)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				_ = writeWS(websocket.TextMessage, buf[:n])
			}
			if readErr != nil {
				return
			}
		}
	}()

	go func() {
		if err := session.Wait(); err != nil && !errors.Is(err, io.EOF) {
			closeAll("ssh closed")
		}
	}()

	for {
		msgType, data, err := ws.ReadMessage()
		if err != nil {
			closeAll("client closed")
			return
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}
		idle.Reset()
		if len(data) == 0 {
			continue
		}
		if data[0] == '{' {
			var resize sshResizeMessage
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" {
				if resize.Cols > 0 && resize.Rows > 0 {
					_ = session.WindowChange(resize.Rows, resize.Cols)
				}
				continue
			}
		}
		if _, err := stdin.Write(data); err != nil {
			closeAll("ssh input error")
			return
		}
	}
}

func canSSHRole(role string) bool {
	return role == middleware.RoleAdmin
}

func (h *Handler) authenticateWS(c *gin.Context) (string, string, error) {
	tokenStr := ""
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		tokenStr = strings.TrimPrefix(auth, "Bearer ")
	}
	if tokenStr == "" {
		tokenStr = strings.TrimSpace(c.Query("token"))
	}
	if tokenStr == "" {
		return "", "", fmt.Errorf("missing token")
	}
	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return h.JWTSecret, nil
	})
	if err != nil || !token.Valid {
		return "", "", fmt.Errorf("invalid token")
	}
	actor := claims.User
	if actor == "" {
		actor = claims.Subject
	}
	if actor == "" {
		actor = "admin"
	}
	return actor, claims.Role, nil
}

func (h *Handler) openSSHShell(host string, port int, user string, privateKeyPEM string) (*ssh.Client, *ssh.Session, io.WriteCloser, io.Reader, io.Reader, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("invalid key format")
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("dial: %v", err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("handshake: %v", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("session: %v", err)
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("pty: %v", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("stdin: %v", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("stdout: %v", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("stderr: %v", err)
	}
	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, nil, nil, nil, nil, fmt.Errorf("shell: %v", err)
	}
	return client, session, stdin, stdout, stderr, nil
}

func sanitizeReason(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "ssh closed"
	}
	if len(msg) > 120 {
		return msg[:120]
	}
	return msg
}
