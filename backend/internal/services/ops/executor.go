package ops

import (
	"context"

	"agr_3x_ui/internal/db"
)

type NodeExecutor interface {
	Reboot(ctx context.Context, node *db.Node) (string, int, error)
	Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error)
}

type UpdateParams struct {
	PrecheckOnly  bool `json:"precheck_only"`
	InstallExpect bool `json:"install_expect"`
}
