package storage

import (
    "context"
    "fmt"
    "io"
    "os"
    "path"
    "strings"

    "github.com/pkg/sftp"
    "golang.org/x/crypto/ssh"
)

type SFTPUploader struct {
    cfg Config
}

func NewSFTP(cfg Config) *SFTPUploader {
    return &SFTPUploader{cfg: cfg}
}

func (u *SFTPUploader) dial() (*sftp.Client, *ssh.Client, error) {
    auth := make([]ssh.AuthMethod, 0, 1)
    if strings.ToLower(u.cfg.AuthMethod) == "key" {
        signer, err := ssh.ParsePrivateKey([]byte(u.cfg.PrivateKeyPEM))
        if err != nil {
            return nil, nil, err
        }
        auth = append(auth, ssh.PublicKeys(signer))
    } else {
        auth = append(auth, ssh.Password(u.cfg.Password))
    }
    client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", u.cfg.Host, u.cfg.Port), &ssh.ClientConfig{
        User:            u.cfg.Username,
        Auth:            auth,
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
        Timeout:         u.cfg.Timeout,
    })
    if err != nil {
        return nil, nil, err
    }
    sftpClient, err := sftp.NewClient(client)
    if err != nil {
        client.Close()
        return nil, nil, err
    }
    return sftpClient, client, nil
}

func (u *SFTPUploader) Test(ctx context.Context) error {
    client, sshClient, err := u.dial()
    if err != nil {
        return err
    }
    defer client.Close()
    defer sshClient.Close()
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    return client.MkdirAll(JoinRemote(u.cfg.BasePath))
}

func (u *SFTPUploader) Upload(ctx context.Context, input UploadInput) error {
    client, sshClient, err := u.dial()
    if err != nil {
        return err
    }
    defer client.Close()
    defer sshClient.Close()
    remoteDir := JoinRemote(u.cfg.BasePath, input.RemoteDir)
    if err := client.MkdirAll(remoteDir); err != nil {
        return err
    }
    file, err := client.Create(path.Join(remoteDir, input.ObjectName))
    if err != nil {
        return err
    }
    defer file.Close()
    done := make(chan error, 1)
    go func() {
        _, err := io.Copy(file, input.Reader)
        done <- err
    }()
    select {
    case <-ctx.Done():
        return ctx.Err()
    case err := <-done:
        return err
    }
}

func (u *SFTPUploader) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
    client, sshClient, err := u.dial()
    if err != nil {
        return nil, err
    }
    defer client.Close()
    defer sshClient.Close()
    root := JoinRemote(u.cfg.BasePath, prefix)
    items := make([]ObjectInfo, 0)
    walker := client.Walk(root)
    for walker.Step() {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }
        if walker.Err() != nil || walker.Stat() == nil || walker.Stat().IsDir() {
            continue
        }
        modified := walker.Stat().ModTime()
        items = append(items, ObjectInfo{Path: strings.TrimPrefix(walker.Path(), "/"), Name: path.Base(walker.Path()), Size: walker.Stat().Size(), ModifiedAt: &modified})
    }
    return items, nil
}

func (u *SFTPUploader) Delete(ctx context.Context, objectPath string) error {
    client, sshClient, err := u.dial()
    if err != nil {
        return err
    }
    defer client.Close()
    defer sshClient.Close()
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    return client.Remove(JoinRemote(u.cfg.BasePath, objectPath))
}

var _ Uploader = (*SFTPUploader)(nil)
var _ = os.FileInfo(nil)
