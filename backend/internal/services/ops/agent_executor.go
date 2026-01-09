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

func (e *AgentExecutor) Reboot(ctx context.Context, node *db.Node) (string, error) {
	return "", errors.New("agent executor not configured")
}

func (e *AgentExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, error) {
	return "", errors.New("agent executor not configured")
}
