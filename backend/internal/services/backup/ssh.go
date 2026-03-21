package backup

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "path"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "github.com/pkg/sftp"
    "golang.org/x/crypto/ssh"

    "agr_3x_ui/internal/db"
)

type remoteClient struct {
    ssh        *ssh.Client
    sftpClient *sftp.Client
    user       string
    sudoPass   string
    usePass    bool
}

func (s *Service) openRemoteClient(node *db.Node) (*remoteClient, error) {
    if node == nil {
        return nil, errors.New("node is required")
    }
    if !node.SSHEnabled {
        return nil, errors.New("ssh is disabled for node")
    }
    if s.Encryptor == nil {
        return nil, errors.New("encryptor is not configured")
    }
    key, err := s.Encryptor.DecryptString(node.SSHKeyEnc)
    if err != nil {
        return nil, err
    }
    signer, err := ssh.ParsePrivateKey([]byte(key))
    if err != nil {
        return nil, err
    }
    cfg := &ssh.ClientConfig{
        User:            node.SSHUser,
        Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
        Timeout:         20 * time.Second,
    }
    addr := net.JoinHostPort(node.SSHHost, strconv.Itoa(node.SSHPort))
    conn, err := net.DialTimeout("tcp", addr, 20*time.Second)
    if err != nil {
        return nil, err
    }
    clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
    if err != nil {
        return nil, err
    }
    client := ssh.NewClient(clientConn, chans, reqs)
    sftpClient, err := sftp.NewClient(client)
    if err != nil {
        client.Close()
        return nil, err
    }
    sudoPass, usePass, err := detectSudo(client, s.SudoPasswords)
    if err != nil {
        sftpClient.Close()
        client.Close()
        return nil, err
    }
    return &remoteClient{ssh: client, sftpClient: sftpClient, user: node.SSHUser, sudoPass: sudoPass, usePass: usePass}, nil
}

func (c *remoteClient) Close() {
    if c == nil {
        return
    }
    if c.sftpClient != nil {
        _ = c.sftpClient.Close()
    }
    if c.ssh != nil {
        _ = c.ssh.Close()
    }
}

func runRemote(ctx context.Context, client *ssh.Client, cmd string) (string, int, error) {
    if client == nil {
        return "", 1, errors.New("ssh client is nil")
    }
    session, err := client.NewSession()
    if err != nil {
        return "", 1, err
    }
    defer session.Close()
    var stdout, stderr bytes.Buffer
    session.Stdout = &stdout
    session.Stderr = &stderr
    if err := session.Start("bash -lc " + strconv.Quote(cmd)); err != nil {
        return "", 1, err
    }
    done := make(chan error, 1)
    go func() { done <- session.Wait() }()
    select {
    case <-ctx.Done():
        return stdout.String() + stderr.String(), 1, ctx.Err()
    case err := <-done:
        if err != nil {
            code := 1
            if exitErr, ok := err.(*ssh.ExitError); ok {
                code = exitErr.ExitStatus()
            }
            return stdout.String() + stderr.String(), code, err
        }
        return stdout.String() + stderr.String(), 0, nil
    }
}

func uploadBytes(ctx context.Context, client *remoteClient, data []byte, remotePath string) error {
    if client == nil || client.sftpClient == nil {
        return errors.New("sftp client is nil")
    }
    dir := path.Dir(remotePath)
    if err := client.sftpClient.MkdirAll(dir); err != nil {
        return err
    }
    file, err := client.sftpClient.Create(remotePath)
    if err != nil {
        return err
    }
    defer file.Close()
    done := make(chan error, 1)
    go func() {
        _, err := io.Copy(file, bytes.NewReader(data))
        done <- err
    }()
    select {
    case <-ctx.Done():
        return ctx.Err()
    case err := <-done:
        return err
    }
}

func downloadFile(ctx context.Context, client *remoteClient, remotePath, localPath string) error {
    if client == nil || client.sftpClient == nil {
        return errors.New("sftp client is nil")
    }
    remote, err := client.sftpClient.Open(remotePath)
    if err != nil {
        return err
    }
    defer remote.Close()
    if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
        return err
    }
    local, err := os.Create(localPath)
    if err != nil {
        return err
    }
    defer local.Close()
    done := make(chan error, 1)
    go func() {
        _, err := io.Copy(local, remote)
        done <- err
    }()
    select {
    case <-ctx.Done():
        return ctx.Err()
    case err := <-done:
        return err
    }
}

func fileSize(client *remoteClient, remotePath string) (int64, error) {
    stat, err := client.sftpClient.Stat(remotePath)
    if err != nil {
        return 0, err
    }
    return stat.Size(), nil
}

func detectSudo(client *ssh.Client, passwords []string) (string, bool, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if _, code, err := runRemote(ctx, client, "sudo -n true"); err == nil && code == 0 {
        return "", false, nil
    }
    for _, pass := range passwords {
        trimmed := strings.TrimSpace(pass)
        if trimmed == "" {
            continue
        }
        cmd := fmt.Sprintf("echo %s | sudo -S -p '' true", shellEscape(trimmed))
        if _, code, err := runRemote(ctx, client, cmd); err == nil && code == 0 {
            return trimmed, true, nil
        }
    }
    return "", false, errors.New("sudo password required")
}

func sudoCmd(command string, pass string, usePass bool) string {
    if usePass && pass != "" {
        return fmt.Sprintf("echo %s | sudo -S -p '' bash -lc %s", shellEscape(pass), strconv.Quote(command))
    }
    return "sudo -n bash -lc " + strconv.Quote(command)
}

func shellEscape(value string) string {
    if value == "" {
        return "''"
    }
    return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
