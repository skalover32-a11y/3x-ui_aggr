package storage

import (
    "context"
    "crypto/tls"
    "fmt"
    "path"
    "strings"

    "github.com/jlaffaye/ftp"
)

type FTPUploader struct {
    cfg Config
}

func NewFTP(cfg Config) *FTPUploader {
    return &FTPUploader{cfg: cfg}
}

func (u *FTPUploader) dial(ctx context.Context) (*ftp.ServerConn, error) {
    addr := fmt.Sprintf("%s:%d", u.cfg.Host, u.cfg.Port)
    options := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(u.cfg.Timeout)}
    if u.cfg.Type == "ftps" {
        tlsConfig := &tls.Config{InsecureSkipVerify: u.cfg.InsecureSkipVerify, ServerName: u.cfg.Host}
        options = append(options, ftp.DialWithExplicitTLS(tlsConfig))
    }
    conn, err := ftp.Dial(addr, options...)
    if err != nil {
        return nil, err
    }
    if err := conn.Login(u.cfg.Username, u.cfg.Password); err != nil {
        _ = conn.Quit()
        return nil, err
    }
    return conn, nil
}

func (u *FTPUploader) Test(ctx context.Context) error {
    conn, err := u.dial(ctx)
    if err != nil {
        return err
    }
    defer conn.Quit()
    if u.cfg.BasePath != "/" {
        _ = conn.MakeDir(u.cfg.BasePath)
    }
    return nil
}

func (u *FTPUploader) Upload(ctx context.Context, input UploadInput) error {
    conn, err := u.dial(ctx)
    if err != nil {
        return err
    }
    defer conn.Quit()
    remoteDir := JoinRemote(u.cfg.BasePath, input.RemoteDir)
    if err := ensureFTPDir(conn, remoteDir); err != nil {
        return err
    }
    return conn.Stor(path.Join(remoteDir, input.ObjectName), input.Reader)
}

func (u *FTPUploader) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
    conn, err := u.dial(ctx)
    if err != nil {
        return nil, err
    }
    defer conn.Quit()
    root := JoinRemote(u.cfg.BasePath, prefix)
    entries := make([]ObjectInfo, 0)
    if err := walkFTP(conn, root, &entries); err != nil {
        if strings.Contains(strings.ToLower(err.Error()), "not found") {
            return entries, nil
        }
        return nil, err
    }
    return entries, nil
}

func (u *FTPUploader) Delete(ctx context.Context, objectPath string) error {
    conn, err := u.dial(ctx)
    if err != nil {
        return err
    }
    defer conn.Quit()
    return conn.Delete(JoinRemote(u.cfg.BasePath, objectPath))
}

func ensureFTPDir(conn *ftp.ServerConn, remoteDir string) error {
    if remoteDir == "/" {
        return nil
    }
    parts := strings.Split(strings.TrimPrefix(remoteDir, "/"), "/")
    current := ""
    for _, part := range parts {
        current = current + "/" + part
        if err := conn.MakeDir(current); err != nil && !strings.Contains(strings.ToLower(err.Error()), "file exists") {
            _ = err
        }
    }
    return nil
}

func walkFTP(conn *ftp.ServerConn, remoteDir string, items *[]ObjectInfo) error {
    list, err := conn.List(remoteDir)
    if err != nil {
        return err
    }
    for _, entry := range list {
        if entry.Name == "." || entry.Name == ".." {
            continue
        }
        fullPath := path.Join(remoteDir, entry.Name)
        if entry.Type == ftp.EntryTypeFolder {
            if err := walkFTP(conn, fullPath, items); err != nil {
                return err
            }
            continue
        }
        modified := entry.Time
        *items = append(*items, ObjectInfo{Path: strings.TrimPrefix(fullPath, "/"), Name: entry.Name, Size: int64(entry.Size), ModifiedAt: &modified})
    }
    return nil
}

var _ Uploader = (*FTPUploader)(nil)
