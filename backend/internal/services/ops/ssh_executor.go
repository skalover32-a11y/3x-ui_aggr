package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
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

func (e *SSHExecutor) Reboot(ctx context.Context, node *db.Node) (string, int, error) {
	return e.runCommand(ctx, node, "sudo /sbin/reboot", true)
}

func (e *SSHExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	if _, code, err := e.runCommand(ctx, node, "command -v x-ui", false); err != nil {
		if code == 0 {
			code = 10
		}
		return "x-ui not installed", code, fmt.Errorf("x-ui not installed")
	}
	if _, code, err := e.runCommand(ctx, node, "command -v expect", false); err != nil {
		if params.InstallExpect {
			if _, _, err := e.runCommand(ctx, node, "sudo apt-get update && sudo apt-get install -y expect", false); err != nil {
				return "failed to install expect", 11, err
			}
		} else {
			if code == 0 {
				code = 11
			}
			return "expect not installed", code, fmt.Errorf("expect not installed")
		}
	}
	if _, code, err := e.runCommand(ctx, node, "command -v expect", false); err != nil {
		if code == 0 {
			code = 11
		}
		return "expect not installed", code, fmt.Errorf("expect not installed")
	}
	if params.PrecheckOnly {
		return "precheck ok", 0, nil
	}
	cmd := buildXUIUpdateCommand()
	return e.runCommand(ctx, node, cmd, false)
}

func (e *SSHExecutor) runCommand(ctx context.Context, node *db.Node, cmd string, allowDisconnect bool) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	if !node.SSHEnabled {
		return "", 1, errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return "", 1, errors.New("unsupported ssh auth method")
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return "", 1, errors.New("ssh config missing")
	}
	if e == nil || e.Encryptor == nil {
		return "", 1, errors.New("encryptor missing")
	}
	key, err := e.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return "", 1, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return "", 1, err
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
		return "", 1, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return "", 1, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", 1, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Start(cmd); err != nil {
		return "", 1, err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return stdoutBuf.String() + stderrBuf.String(), 1, ctx.Err()
	case err := <-waitCh:
		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			}
			if allowDisconnect && isDisconnectError(err) {
				return stdoutBuf.String() + stderrBuf.String(), 0, nil
			}
			return stdoutBuf.String() + stderrBuf.String(), exitCode, err
		}
		return stdoutBuf.String() + stderrBuf.String(), 0, nil
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

func buildXUIUpdateCommand() string {
	script := `flock -n /var/lock/x-ui-update.lock -c "expect <<'EOF'
set timeout 60
set env(TERM) \"dumb\"
log_user 1
match_max 200000
spawn x-ui
expect {
  -re {Please enter your selection.*} { send \"2\r\" }
  -re {Enter.*selection.*} { send \"2\r\" }
  -re {Enter.*choice.*} { send \"2\r\" }
  timeout { puts \"ERROR: timeout waiting for menu\"; exit 2 }
}
set timeout 900
expect {
  -re {Already.*latest} { puts \"INFO: already latest version\"; exit 0 }
  -re {Update.*(completed|success|finished)} { puts \"INFO: update completed\"; exit 0 }
  -re {Please enter your selection.*} { puts \"INFO: update finished, returned to menu\"; exit 0 }
  eof { puts \"INFO: x-ui exited after update\"; exit 0 }
  timeout { puts \"ERROR: update timeout\"; exit 3 }
}
EOF"
rc=$?
if [ $rc -eq 1 ]; then
  exit 20
fi
exit $rc
`
	return "bash -lc " + strconv.Quote(script)
}
