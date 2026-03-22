package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ParsedEvent struct {
	ProjectName         string
	SessionID           string
	Slug                string
	TranscriptPath      string
	Type                string
	Subtype             string
	ToolName            string
	ToolUseID           string
	Timestamp           int64
	OwnerAgentID        string
	SubAgentID          string
	SubAgentName        string
	SubAgentDescription string
	Metadata            map[string]any
	Raw                 map[string]any
}

type Project struct {
	ID             int64
	Slug           string
	Name           string
	TranscriptPath string
	Metadata       string
	SessionCount   int64
	CreatedAt      int64
	UpdatedAt      int64
}

type Session struct {
	ID              string
	ParentSessionID string
	ProjectID       int64
	ProjectSlug     string
	ProjectName     string
	Slug            string
	Status          string
	Runtime         string
	StartedAt      int64
	StoppedAt      int64
	TranscriptPath string
	Metadata       string
	EventCount     int64
	AgentCount     int64
	LastActivity   int64
	CreatedAt      int64
	UpdatedAt      int64
}

type Agent struct {
	ID             string
	SessionID      string
	ParentAgentID  string
	Name           string
	Description    string
	AgentType      string
	AgentClass     string
	TranscriptPath string
	Metadata       string
	CreatedAt      int64
	UpdatedAt      int64
}

type Event struct {
	ID        int64
	AgentID   string
	SessionID string
	Type      string
	Subtype   string
	ToolName  string
	ToolUseID string
	Timestamp int64
	CreatedAt int64
	Payload   string
}

type EventFilter struct {
	AgentIDs []string
	Type     string
	Subtype  string
	Search   string
	Limit    int
	Offset   int
}

type PendingAgentSpawn struct {
	ToolUseID    string
	SessionID    string
	OwnerAgentID string
	Name         string
	Description  string
	AgentType    string
	CreatedAt    int64
}

func DeriveEventStatus(subtype string) string {
	switch subtype {
	case "PreToolUse":
		return "running"
	case "PostToolUse":
		return "completed"
	case "PostToolUseFailure":
		return "failed"
	default:
		return "pending"
	}
}

func (e Event) PayloadPretty() string {
	if strings.TrimSpace(e.Payload) == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(e.Payload), &v); err != nil {
		return e.Payload
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return e.Payload
	}
	return strings.TrimSpace(buf.String())
}

func EventSummary(e Event) string {
	var parts []string
	if e.Subtype != "" {
		parts = append(parts, e.Subtype)
	}
	if e.ToolName != "" {
		parts = append(parts, fmt.Sprintf("tool=%s", e.ToolName))
	}
	if len(parts) == 0 {
		parts = append(parts, e.Type)
	}
	return strings.Join(parts, " ")
}
