package storage

import (
    "context"
    "io"
    "path"
    "strings"
    "time"
)

type TargetType string

type Config struct {
    Type               string
    Name               string
    Host               string
    Port               int
    Username           string
    Password           string
    BasePath           string
    Timeout            time.Duration
    PassiveMode        bool
    InsecureSkipVerify bool
    AuthMethod         string
    PrivateKeyPEM      string
    URL                string
    Bucket             string
    Region             string
    AccessKey          string
    SecretKey          string
    UseSSL             bool
    PathStyle          bool
    LocalPath          string
}

type ObjectInfo struct {
    Path         string
    Name         string
    Size         int64
    ModifiedAt   *time.Time
}

type UploadInput struct {
    RemoteDir   string
    ObjectName  string
    Reader      io.Reader
    Size        int64
    ContentType string
}

type Uploader interface {
    Test(ctx context.Context) error
    Upload(ctx context.Context, input UploadInput) error
    List(ctx context.Context, prefix string) ([]ObjectInfo, error)
    Delete(ctx context.Context, objectPath string) error
}

func JoinRemote(parts ...string) string {
    clean := make([]string, 0, len(parts))
    for _, part := range parts {
        trimmed := strings.TrimSpace(part)
        if trimmed == "" {
            continue
        }
        clean = append(clean, trimmed)
    }
    if len(clean) == 0 {
        return "/"
    }
    joined := path.Join(clean...)
    if !strings.HasPrefix(joined, "/") {
        return "/" + joined
    }
    return joined
}
