package backup

import "agr_3x_ui/internal/services/backup/storage"

func NewUploader(cfg storage.Config) (storage.Uploader, error) {
    return storage.New(cfg)
}
