package storage

import (
    "context"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type LocalUploader struct {
    cfg Config
}

func NewLocal(cfg Config) *LocalUploader {
    return &LocalUploader{cfg: cfg}
}

func (u *LocalUploader) Test(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    return os.MkdirAll(u.cfg.LocalPath, 0o755)
}

func (u *LocalUploader) Upload(ctx context.Context, input UploadInput) error {
    targetDir := filepath.Join(u.cfg.LocalPath, strings.TrimPrefix(filepath.FromSlash(input.RemoteDir), string(filepath.Separator)))
    if err := os.MkdirAll(targetDir, 0o755); err != nil {
        return err
    }
    filePath := filepath.Join(targetDir, input.ObjectName)
    tmpPath := filePath + ".tmp"
    file, err := os.Create(tmpPath)
    if err != nil {
        return err
    }
    defer file.Close()
    done := make(chan error, 1)
    go func() {
        _, err := io.Copy(file, input.Reader)
        if err == nil {
            err = file.Sync()
        }
        done <- err
    }()
    select {
    case <-ctx.Done():
        _ = os.Remove(tmpPath)
        return ctx.Err()
    case err := <-done:
        if err != nil {
            _ = os.Remove(tmpPath)
            return err
        }
    }
    return os.Rename(tmpPath, filePath)
}

func (u *LocalUploader) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
    root := filepath.Join(u.cfg.LocalPath, strings.TrimPrefix(filepath.FromSlash(prefix), string(filepath.Separator)))
    entries := make([]ObjectInfo, 0)
    err := filepath.Walk(root, func(filePath string, info os.FileInfo, err error) error {
        if err != nil || info == nil || info.IsDir() {
            return err
        }
        modified := info.ModTime()
        rel, _ := filepath.Rel(u.cfg.LocalPath, filePath)
        entries = append(entries, ObjectInfo{
            Path:       filepath.ToSlash(rel),
            Name:       info.Name(),
            Size:       info.Size(),
            ModifiedAt: &modified,
        })
        return nil
    })
    if err != nil && !os.IsNotExist(err) {
        return nil, err
    }
    return entries, nil
}

func (u *LocalUploader) Delete(ctx context.Context, objectPath string) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }
    return os.Remove(filepath.Join(u.cfg.LocalPath, strings.TrimPrefix(filepath.FromSlash(objectPath), string(filepath.Separator))))
}

var _ Uploader = (*LocalUploader)(nil)
