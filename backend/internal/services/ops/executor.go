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

// AgentInstallProbe is an optional capability for executors that can verify
// whether vlf-agent is actually active on a node.
type AgentInstallProbe interface {
	CheckAgentInstalled(ctx context.Context, node *db.Node, agentPort int) (installed bool, details string, err error)
}

type UpdateParams struct {
	PrecheckOnly  bool `json:"precheck_only"`
	InstallExpect bool `json:"install_expect"`
}

type DeployAgentParams struct {
	BinaryPath         string
	ServiceContent     []byte
	ConfigContent      []byte
	AgentPort          int
	AllowCIDR          string
	Token              string
	MetricsRequireAuth bool
	EnableUFW          bool
	HealthCheck        bool
	InstallDocker      bool
	SudoPasswords      []string
	NodeHost           string
	PreLog             string
}
