package ops

import (
	"context"
	"errors"

	"agr_3x_ui/internal/db"
)

type AgentExecutor struct {
}

func NewAgentExecutor() *AgentExecutor {
	return &AgentExecutor{}
}

func (e *AgentExecutor) Reboot(ctx context.Context, node *db.Node) (string, int, error) {
	return "", 1, errors.New("agent executor not configured")
}

func (e *AgentExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	return "", 1, errors.New("agent executor not configured")
}

func (e *AgentExecutor) DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error) {
	return "", 1, errors.New("agent executor not configured")
}
