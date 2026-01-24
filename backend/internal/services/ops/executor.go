package ops

import (
	"context"

	"agr_3x_ui/internal/db"
)

type NodeExecutor interface {
	Reboot(ctx context.Context, node *db.Node) (string, int, error)
	Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error)
	DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error)
	RestartService(ctx context.Context, node *db.Node, service string) (string, int, error)
}

type UpdateParams struct {
	PrecheckOnly  bool `json:"precheck_only"`
	InstallExpect bool `json:"install_expect"`
}

type DeployAgentParams struct {
	BinaryPath     string
	ServiceContent []byte
	ConfigContent  []byte
	AgentPort      int
	AllowCIDR      string
	Token          string
	EnableUFW      bool
	HealthCheck    bool
	InstallDocker  bool
	SudoPasswords  []string
	NodeHost       string
	PreLog         string
}
