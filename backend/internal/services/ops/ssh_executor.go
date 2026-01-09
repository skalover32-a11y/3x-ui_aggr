package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
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
	if params.PrecheckOnly {
		return e.runUpdatePrecheck(ctx, node, params)
	}
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
	cmd := buildXUIUpdateCommand()
	return e.runCommand(ctx, node, cmd, false)
}

func (e *SSHExecutor) DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	client, err := e.openClient(node)
	if err != nil {
		return "", 1, err
	}
	defer client.Close()

	logs := &strings.Builder{}
	writeLog(logs, "preflight ok")

	sudoPass, usePass, err := detectSudo(ctx, client, params.SudoPasswords)
	if err != nil {
		writeLog(logs, "sudo check failed")
		return logs.String(), 2, err
	}

	if params.BinaryPath == "" {
		return logs.String(), 3, errors.New("binary path missing")
	}
	if err := uploadFile(ctx, client, params.BinaryPath, "/tmp/vlf-agent"); err != nil {
		writeLog(logs, "upload agent failed")
		return logs.String(), 4, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 755 /tmp/vlf-agent /usr/local/bin/vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "install agent binary failed")
		return logs.String(), 5, err
	}
	writeLog(logs, "agent binary installed")

	if err := uploadBytes(ctx, client, params.ConfigContent, "/tmp/vlf-agent.yaml"); err != nil {
		writeLog(logs, "upload config failed")
		return logs.String(), 6, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("mkdir -p /etc/vlf-agent", sudoPass, usePass)); err != nil {
		return logs.String(), 7, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 600 /tmp/vlf-agent.yaml /etc/vlf-agent/config.yaml", sudoPass, usePass)); err != nil {
		writeLog(logs, "install config failed")
		return logs.String(), 8, err
	}
	writeLog(logs, "config installed")

	if err := uploadBytes(ctx, client, params.ServiceContent, "/tmp/vlf-agent.service"); err != nil {
		writeLog(logs, "upload service failed")
		return logs.String(), 9, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 644 /tmp/vlf-agent.service /etc/systemd/system/vlf-agent.service", sudoPass, usePass)); err != nil {
		writeLog(logs, "install service failed")
		return logs.String(), 10, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl daemon-reload", sudoPass, usePass)); err != nil {
		return logs.String(), 11, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl enable --now vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "enable service failed")
		return logs.String(), 12, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl restart vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "restart service failed")
		return logs.String(), 13, err
	}
	writeLog(logs, "service started")

	if params.EnableUFW && params.AllowCIDR != "" && params.AgentPort > 0 {
		cmd := fmt.Sprintf("ufw allow from %s to any port %d proto tcp", params.AllowCIDR, params.AgentPort)
		if _, _, err := runRemote(ctx, client, sudoCmd(cmd, sudoPass, usePass)); err != nil {
			writeLog(logs, "ufw rule failed")
			return logs.String(), 14, err
		}
		writeLog(logs, "ufw rule applied")
	}

	if params.HealthCheck && params.AgentPort > 0 {
		cmd := fmt.Sprintf("curl -fsS http://127.0.0.1:%d/health", params.AgentPort)
		if _, _, err := runRemote(ctx, client, cmd); err != nil {
			writeLog(logs, "health check failed")
			return logs.String(), 15, err
		}
		writeLog(logs, "health check ok")
	}

	return logs.String(), 0, nil
}

func (e *SSHExecutor) openClient(node *db.Node) (*ssh.Client, error) {
	if node == nil {
		return nil, errors.New("node missing")
	}
	if !node.SSHEnabled {
		return nil, errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return nil, errors.New("unsupported ssh auth method")
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return nil, errors.New("ssh config missing")
	}
	if e == nil || e.Encryptor == nil {
		return nil, errors.New("encryptor missing")
	}
	key, err := e.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, err
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
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func runRemote(ctx context.Context, client *ssh.Client, cmd string) (string, int, error) {
	if client == nil {
		return "", 1, errors.New("ssh client missing")
	}
	session, err := client.NewSession()
	if err != nil {
		return "", 1, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Start("bash -lc " + strconv.Quote(cmd)); err != nil {
		return "", 1, err
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return stdoutBuf.String() + stderrBuf.String(), 1, ctx.Err()
	case err := <-done:
		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			}
			return stdoutBuf.String() + stderrBuf.String(), exitCode, err
		}
		return stdoutBuf.String() + stderrBuf.String(), 0, nil
	}
}

func detectSudo(ctx context.Context, client *ssh.Client, passwords []string) (string, bool, error) {
	if _, code, err := runRemote(ctx, client, "sudo -n true"); err == nil && code == 0 {
		return "", false, nil
	}
	for _, pass := range passwords {
		trim := strings.TrimSpace(pass)
		if trim == "" {
			continue
		}
		cmd := fmt.Sprintf("echo %s | sudo -S -p '' true", shellEscape(trim))
		if _, code, err := runRemote(ctx, client, cmd); err == nil && code == 0 {
			return trim, true, nil
		}
	}
	return "", false, errors.New("sudo password required")
}

func sudoCmd(cmd string, pass string, usePass bool) string {
	if usePass && pass != "" {
		return fmt.Sprintf("echo %s | sudo -S -p '' %s", shellEscape(pass), cmd)
	}
	return "sudo -n " + cmd
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\'"\'"'`) + "'"
}

func uploadFile(ctx context.Context, client *ssh.Client, localPath string, remotePath string) error {
	if client == nil {
		return errors.New("ssh client missing")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return uploadReader(ctx, client, file, remotePath)
}

func uploadBytes(ctx context.Context, client *ssh.Client, data []byte, remotePath string) error {
	if len(data) == 0 {
		return errors.New("empty payload")
	}
	return uploadReader(ctx, client, bytes.NewReader(data), remotePath)
}

func uploadReader(ctx context.Context, client *ssh.Client, reader io.Reader, remotePath string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sftpClient.Close()
	remote, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remote.Close()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(remote, reader)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func writeLog(buf *strings.Builder, line string) {
	if buf == nil {
		return
	}
	if buf.Len() > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString(line)
}

func (e *SSHExecutor) runUpdatePrecheck(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	var lines []string
	exitCode := 0

	xuiOut, _, xuiErr := e.runCommand(ctx, node, "command -v x-ui", false)
	if xuiErr != nil {
		if isExitError(xuiErr) {
			lines = append(lines, "ERR: x-ui missing")
			exitCode = 10
		} else {
			return xuiOut, 10, xuiErr
		}
	} else {
		lines = append(lines, "OK: x-ui present")
	}

	expectOut, _, expectErr := e.runCommand(ctx, node, "command -v expect", false)
	if expectErr != nil {
		if isExitError(expectErr) {
			lines = append(lines, "ERR: expect missing")
		} else {
			return expectOut, 11, expectErr
		}
	} else {
		lines = append(lines, "OK: expect present")
	}
	if params.InstallExpect {
		lines = append(lines, "INFO: install_expect requested, skipped in precheck_only")
	}

	versionOut, _, _ := e.runCommand(ctx, node, "bash -lc \"x-ui version || true\"", false)
	if strings.TrimSpace(versionOut) != "" {
		lines = append(lines, "x-ui version: "+strings.TrimSpace(versionOut))
	}

	sudoOut, _, sudoErr := e.runCommand(ctx, node, "sudo -n true", false)
	if sudoErr != nil {
		if isExitError(sudoErr) {
			lines = append(lines, "ERR: sudo -n failed (passwordless sudo missing)")
		} else {
			return sudoOut, 12, sudoErr
		}
	} else {
		lines = append(lines, "OK: sudo -n available")
	}

	logText := strings.Join(lines, "\n")
	if exitCode != 0 {
		return logText, exitCode, fmt.Errorf("precheck failed")
	}
	return logText, 0, nil
}

func isExitError(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr)
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
