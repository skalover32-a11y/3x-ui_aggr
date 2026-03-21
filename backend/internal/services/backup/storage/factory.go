package storage

import "fmt"

func New(cfg Config) (Uploader, error) {
    switch cfg.Type {
    case "ftp", "ftps":
        return NewFTP(cfg), nil
    case "sftp":
        return NewSFTP(cfg), nil
    case "webdav":
        return NewWebDAV(cfg), nil
    case "s3":
        return NewS3(cfg), nil
    case "local":
        return NewLocal(cfg), nil
    default:
        return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
    }
}
