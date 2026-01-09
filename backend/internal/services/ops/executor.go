package ops

import (
	"context"

	"agr_3x_ui/internal/db"
)

type NodeExecutor interface {
	Reboot(ctx context.Context, node *db.Node) (string, error)
	Update(ctx context.Context, node *db.Node, params UpdateParams) (string, error)
}

type UpdateParams struct {
	PrecheckOnly  bool
	InstallExpect bool
}
