package app

import "github.com/chojs23/lazyagent/internal/model"

type IngestResult struct {
	EventID   int64  `json:"event_id"`
	SessionID string `json:"session_id"`
	ProjectID int64  `json:"project_id"`
}

func agentField(a *model.Agent, fn func(*model.Agent) string) string {
	if a == nil {
		return ""
	}
	return fn(a)
}

func pendingField(p *model.PendingAgentSpawn, fn func(*model.PendingAgentSpawn) string) string {
	if p == nil {
		return ""
	}
	return fn(p)
}
