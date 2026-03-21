package storage

import (
    "context"
    "path"
    "strings"

    minio "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Uploader struct {
    cfg Config
}

func NewS3(cfg Config) *S3Uploader {
    return &S3Uploader{cfg: cfg}
}

func (u *S3Uploader) client() (*minio.Client, error) {
    return minio.New(u.cfg.Host, &minio.Options{
        Creds:  credentials.NewStaticV4(u.cfg.AccessKey, u.cfg.SecretKey, ""),
        Secure: u.cfg.UseSSL,
        Region: u.cfg.Region,
    })
}

func (u *S3Uploader) ensureBucket(ctx context.Context, client *minio.Client) error {
    exists, err := client.BucketExists(ctx, u.cfg.Bucket)
    if err != nil {
        return err
    }
    if exists {
        return nil
    }
    return client.MakeBucket(ctx, u.cfg.Bucket, minio.MakeBucketOptions{Region: u.cfg.Region})
}

func (u *S3Uploader) Test(ctx context.Context) error {
    client, err := u.client()
    if err != nil {
        return err
    }
    return u.ensureBucket(ctx, client)
}

func (u *S3Uploader) Upload(ctx context.Context, input UploadInput) error {
    client, err := u.client()
    if err != nil {
        return err
    }
    if err := u.ensureBucket(ctx, client); err != nil {
        return err
    }
    objectPath := strings.TrimPrefix(path.Join(strings.TrimPrefix(u.cfg.BasePath, "/"), input.RemoteDir, input.ObjectName), "/")
    _, err = client.PutObject(ctx, u.cfg.Bucket, objectPath, input.Reader, input.Size, minio.PutObjectOptions{})
    return err
}

func (u *S3Uploader) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
    client, err := u.client()
    if err != nil {
        return nil, err
    }
    objectPrefix := strings.TrimPrefix(path.Join(strings.TrimPrefix(u.cfg.BasePath, "/"), prefix), "/")
    items := make([]ObjectInfo, 0)
    for object := range client.ListObjects(ctx, u.cfg.Bucket, minio.ListObjectsOptions{Prefix: objectPrefix, Recursive: true}) {
        if object.Err != nil {
            return nil, object.Err
        }
        modified := object.LastModified
        items = append(items, ObjectInfo{Path: object.Key, Name: path.Base(object.Key), Size: object.Size, ModifiedAt: &modified})
    }
    return items, nil
}

func (u *S3Uploader) Delete(ctx context.Context, objectPath string) error {
    client, err := u.client()
    if err != nil {
        return err
    }
    return client.RemoveObject(ctx, u.cfg.Bucket, strings.TrimPrefix(path.Join(strings.TrimPrefix(u.cfg.BasePath, "/"), objectPath), "/"), minio.RemoveObjectOptions{})
}

var _ Uploader = (*S3Uploader)(nil)
