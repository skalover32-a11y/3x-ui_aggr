package storage

import (
    "bytes"
    "context"
    "crypto/tls"
    "net/http"
    "path"
    "strings"

    "github.com/studio-b12/gowebdav"
)

type WebDAVUploader struct {
    cfg Config
}

func NewWebDAV(cfg Config) *WebDAVUploader {
    return &WebDAVUploader{cfg: cfg}
}

func (u *WebDAVUploader) client() *gowebdav.Client {
    client := gowebdav.NewClient(u.cfg.URL, u.cfg.Username, u.cfg.Password)
    client.SetTransport(&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: u.cfg.InsecureSkipVerify}})
    return client
}

func (u *WebDAVUploader) Test(ctx context.Context) error {
    client := u.client()
    return client.MkdirAll(JoinRemote(u.cfg.BasePath), 0o755)
}

func (u *WebDAVUploader) Upload(ctx context.Context, input UploadInput) error {
    client := u.client()
    remoteDir := JoinRemote(u.cfg.BasePath, input.RemoteDir)
    if err := client.MkdirAll(remoteDir, 0o755); err != nil {
        return err
    }
    buf := &bytes.Buffer{}
    if _, err := buf.ReadFrom(input.Reader); err != nil {
        return err
    }
    return client.Write(path.Join(remoteDir, input.ObjectName), buf.Bytes(), 0o644)
}

func (u *WebDAVUploader) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
    client := u.client()
    root := JoinRemote(u.cfg.BasePath, prefix)
    entries, err := client.ReadDir(root)
    if err != nil {
        return nil, err
    }
    items := make([]ObjectInfo, 0, len(entries))
    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }
        modified := entry.ModTime()
        items = append(items, ObjectInfo{Path: strings.TrimPrefix(path.Join(root, entry.Name()), "/"), Name: entry.Name(), Size: entry.Size(), ModifiedAt: &modified})
    }
    return items, nil
}

func (u *WebDAVUploader) Delete(ctx context.Context, objectPath string) error {
    client := u.client()
    return client.Remove(JoinRemote(u.cfg.BasePath, objectPath))
}

var _ Uploader = (*WebDAVUploader)(nil)
