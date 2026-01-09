package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

type SSHExecutor struct {
	Encryptor *security.Encryptor
	Timeout   time.Duration
}

func NewSSHExecutor(enc *security.Encryptor, timeout time.Duration) *SSHExecutor {
	return &SSHExecutor{Encryptor: enc, Timeout: timeout}
}

func (e *SSHExecutor) Reboot(ctx context.Context, node *db.Node) (string, error) {
	return e.runCommand(ctx, node, "sudo /sbin/reboot", true)
}

func (e *SSHExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, error) {
	if params.PrecheckOnly {
		out, err := e.runCommand(ctx, node, "command -v expect", false)
		if err != nil {
			return out, fmt.Errorf("expect not installed")
		}
		return out, nil
	}
	if params.InstallExpect {
		if _, err := e.runCommand(ctx, node, "sudo apt-get update && sudo apt-get install -y expect", false); err != nil {
			return "", err
		}
	}
	if _, err := e.runCommand(ctx, node, "command -v expect", false); err != nil {
		return "", fmt.Errorf("expect not installed")
	}
	script := `expect -c 'set timeout 900; spawn x-ui; expect -re "(?i)select|option|choice"; send "2\r"; expect eof'`
	return e.runCommand(ctx, node, script, false)
}

func (e *SSHExecutor) runCommand(ctx context.Context, node *db.Node, cmd string, allowDisconnect bool) (string, error) {
	if node == nil {
		return "", errors.New("node missing")
	}
	if !node.SSHEnabled {
		return "", errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return "", errors.New("unsupported ssh auth method")
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return "", errors.New("ssh config missing")
	}
	if e == nil || e.Encryptor == nil {
		return "", errors.New("encryptor missing")
	}
	key, err := e.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return "", err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return "", err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            node.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return "", err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Start(cmd); err != nil {
		return "", err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return stdoutBuf.String() + stderrBuf.String(), ctx.Err()
	case err := <-waitCh:
		if err != nil {
			if allowDisconnect && isDisconnectError(err) {
				return stdoutBuf.String() + stderrBuf.String(), nil
			}
			return stdoutBuf.String() + stderrBuf.String(), err
		}
		return stdoutBuf.String() + stderrBuf.String(), nil
	}
}

func isDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset") || strings.Contains(msg, "closed network connection") || strings.Contains(msg, "eof") {
		return true
	}
	return false
}
