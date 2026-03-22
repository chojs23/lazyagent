package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/opencode"
	"github.com/chojs23/lazyagent/internal/store"
)

type IngestResult struct {
	EventID   int64  `json:"event_id"`
	SessionID string `json:"session_id"`
	ProjectID int64  `json:"project_id"`
}

func IngestClaudeEvent(ctx context.Context, st *store.Store, payload map[string]any, projectSlugOverride string) (IngestResult, error) {
	parsed := claude.ParseRawEvent(payload)
	result := IngestResult{SessionID: parsed.SessionID}

	err := st.WithTx(ctx, func(q *store.Queries) error {
		existingSession, err := q.GetSessionByID(ctx, parsed.SessionID)
		if err != nil {
			return err
		}

		var projectID int64
		if existingSession != nil {
			projectID = existingSession.ProjectID
		} else {
			id, err := resolveProject(ctx, q, projectSlugOverride, parsed.TranscriptPath)
			if err != nil {
				return err
			}
			projectID = id
		}

		if err := q.UpsertSession(ctx, parsed.SessionID, projectID, parsed.Slug, "claude", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		if parsed.TranscriptPath != "" {
			session, err := q.GetSessionByID(ctx, parsed.SessionID)
			if err != nil {
				return err
			}
			if session != nil && session.Slug == "" {
				if slug := loadSessionSlug(parsed.TranscriptPath); slug != "" {
					if err := q.UpdateSessionSlug(ctx, parsed.SessionID, slug); err != nil {
						return err
					}
				}
			}
		}

		rootAgentID := parsed.SessionID
		if err := q.UpsertAgent(ctx, rootAgentID, parsed.SessionID, "", "", "", "", ""); err != nil {
			return err
		}

		if parsed.Subtype == "PreToolUse" && parsed.ToolName == "Agent" {
			toolInput := asMap(payload["tool_input"])
			if err := q.AddPendingAgentSpawn(ctx, model.PendingAgentSpawn{
				ToolUseID:    parsed.ToolUseID,
				SessionID:    parsed.SessionID,
				OwnerAgentID: parsed.OwnerAgentID,
				Name:         parsed.SubAgentName,
				Description:  parsed.SubAgentDescription,
				AgentType:    str(toolInput["subagent_type"]),
			}); err != nil {
				return err
			}
		}

		agentID := rootAgentID
		if parsed.OwnerAgentID != "" {
			agentID = parsed.OwnerAgentID
		}

		if parsed.OwnerAgentID != "" && parsed.OwnerAgentID != rootAgentID {
			existingAgent, err := q.GetAgentByID(ctx, parsed.OwnerAgentID)
			if err != nil {
				return err
			}

			var pending *model.PendingAgentSpawn
			if existingAgent == nil || (existingAgent.Name == "" && existingAgent.Description == "") {
				pending, err = q.PopPendingAgentQueue(ctx, parsed.SessionID)
				if err != nil {
					return err
				}
			}

			name := pick(agentField(existingAgent, func(a *model.Agent) string { return a.Name }), pendingField(pending, func(p *model.PendingAgentSpawn) string { return p.Name }))
			desc := pick(agentField(existingAgent, func(a *model.Agent) string { return a.Description }), pendingField(pending, func(p *model.PendingAgentSpawn) string { return p.Description }))

			if err := q.UpsertAgent(ctx, parsed.OwnerAgentID, parsed.SessionID, rootAgentID, name, desc,
				str(payload["agent_type"]), str(payload["agent_transcript_path"])); err != nil {
				return err
			}
		}

		if parsed.SubAgentID != "" {
			subName := parsed.SubAgentName
			subDesc := parsed.SubAgentDescription
			subType := str(payload["agent_type"])

			if parsed.Subtype == "PostToolUse" && parsed.ToolName == "Agent" && parsed.ToolUseID != "" {
				pending, err := q.TakePendingAgentSpawn(ctx, parsed.ToolUseID)
				if err != nil {
					return err
				}
				if pending != nil {
					subName = pick(subName, pending.Name)
					subDesc = pick(subDesc, pending.Description)
					subType = pick(subType, pending.AgentType)
				}

				toolInput := asMap(payload["tool_input"])
				toolResp := asMap(payload["tool_response"])
				subType = pick(subType,
					str(toolInput["subagent_type"]),
					str(toolResp["agentType"]),
					str(toolResp["subagent_type"]))
			}

			if err := q.UpsertAgent(ctx, parsed.SubAgentID, parsed.SessionID, rootAgentID, subName, subDesc, subType, ""); err != nil {
				return err
			}

			if parsed.Subtype == "agent_progress" {
				agentID = parsed.SubAgentID
			}
		}

		if parsed.Subtype == "SessionEnd" {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "stopped"); err != nil {
				return err
			}
		} else {
			session, err := q.GetSessionByID(ctx, parsed.SessionID)
			if err != nil {
				return err
			}
			if session != nil && session.Status == "stopped" {
				if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "active"); err != nil {
					return err
				}
			}
		}

		raw, err := json.Marshal(parsed.Raw)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}

		eventID, err := q.InsertEvent(ctx, model.Event{
			AgentID:   agentID,
			SessionID: parsed.SessionID,
			Type:      parsed.Type,
			Subtype:   parsed.Subtype,
			ToolName:  parsed.ToolName,
			ToolUseID: parsed.ToolUseID,
			Timestamp: parsed.Timestamp,
			Payload:   string(raw),
		})
		if err != nil {
			return err
		}

		result.EventID = eventID
		result.ProjectID = projectID
		return nil
	})

	return result, err
}

func IngestOpenCodeEvent(ctx context.Context, st *store.Store, payload map[string]any, projectSlugOverride string) (IngestResult, error) {
	parsed := opencode.ParseRawEvent(payload)
	result := IngestResult{SessionID: parsed.SessionID}

	err := st.WithTx(ctx, func(q *store.Queries) error {
		existingSession, err := q.GetSessionByID(ctx, parsed.SessionID)
		if err != nil {
			return err
		}

		var projectID int64
		if existingSession != nil {
			projectID = existingSession.ProjectID
		} else {
			slug := projectSlugOverride
			if slug == "" && parsed.ProjectName != "" {
				slug = deriveSlug(parsed.ProjectName)
			}
			id, err := resolveProject(ctx, q, slug, parsed.TranscriptPath)
			if err != nil {
				return err
			}
			projectID = id
		}

		if err := q.UpsertSession(ctx, parsed.SessionID, projectID, "", "opencode", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		// root agent = session ID
		rootAgentID := parsed.SessionID
		if err := q.UpsertAgent(ctx, rootAgentID, parsed.SessionID, "", "", "", "", ""); err != nil {
			return err
		}

		agentID := rootAgentID
		// subagent (child session)
		if parsed.SubAgentID != "" {
			if err := q.UpsertAgent(ctx, parsed.SubAgentID, parsed.SessionID, rootAgentID, parsed.SubAgentName, parsed.SubAgentDescription, "", ""); err != nil {
				return err
			}
			agentID = parsed.SubAgentID
		} else if parsed.OwnerAgentID != "" && parsed.OwnerAgentID != rootAgentID {
			agentID = parsed.OwnerAgentID
			if err := q.UpsertAgent(ctx, agentID, parsed.SessionID, rootAgentID, "", "", "", ""); err != nil {
				return err
			}
		}

		// session lifecycle
		if parsed.Subtype == "SessionEnd" {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "stopped"); err != nil {
				return err
			}
		} else if existingSession != nil && existingSession.Status == "stopped" {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "active"); err != nil {
				return err
			}
		}

		raw, err := json.Marshal(parsed.Raw)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}

		eventID, err := q.InsertEvent(ctx, model.Event{
			AgentID:   agentID,
			SessionID: parsed.SessionID,
			Type:      parsed.Type,
			Subtype:   parsed.Subtype,
			ToolName:  parsed.ToolName,
			ToolUseID: parsed.ToolUseID,
			Timestamp: parsed.Timestamp,
			Payload:   string(raw),
		})
		if err != nil {
			return err
		}

		result.EventID = eventID
		result.ProjectID = projectID
		return nil
	})

	return result, err
}

func resolveProject(ctx context.Context, q *store.Queries, slugOverride, transcriptPath string) (int64, error) {
	if slugOverride != "" {
		proj, err := q.GetProjectBySlug(ctx, slugOverride)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			return proj.ID, nil
		}
		dir := ""
		if transcriptPath != "" {
			dir = extractProjectDir(transcriptPath)
		}
		return q.CreateProject(ctx, slugOverride, slugOverride, dir)
	}

	if transcriptPath != "" {
		dir := extractProjectDir(transcriptPath)
		proj, err := q.GetProjectByTranscriptPath(ctx, dir)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			return proj.ID, nil
		}

		candidates := deriveSlugCandidates(transcriptPath)
		for _, c := range candidates {
			avail, err := q.IsSlugAvailable(ctx, c)
			if err != nil {
				return 0, err
			}
			if avail {
				return q.CreateProject(ctx, c, c, dir)
			}
		}

		base := candidates[0]
		for suffix := 2; ; suffix++ {
			c := fmt.Sprintf("%s-%d", base, suffix)
			avail, err := q.IsSlugAvailable(ctx, c)
			if err != nil {
				return 0, err
			}
			if avail {
				return q.CreateProject(ctx, c, c, dir)
			}
		}
	}

	proj, err := q.GetProjectBySlug(ctx, "unknown")
	if err != nil {
		return 0, err
	}
	if proj != nil {
		return proj.ID, nil
	}
	return q.CreateProject(ctx, "unknown", "unknown", "")
}

func extractProjectDir(transcriptPath string) string {
	cleaned := strings.TrimRight(transcriptPath, "/")
	if ext := filepath.Ext(cleaned); ext != "" {
		return filepath.Dir(cleaned)
	}
	return cleaned
}

func deriveSlug(pathOrDir string) string {
	candidates := deriveSlugCandidates(pathOrDir)
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "unknown"
}

func deriveSlugCandidates(pathOrDir string) []string {
	dir := extractProjectDir(pathOrDir)
	encoded := filepath.Base(dir)
	var parts []string
	for _, p := range strings.Split(encoded, "-") {
		if p != "" {
			parts = append(parts, strings.ToLower(p))
		}
	}
	if len(parts) == 0 {
		return []string{"unknown"}
	}
	if len(parts) == 1 {
		return []string{parts[0]}
	}

	var candidates []string
	for size := 1; size <= len(parts); size++ {
		candidates = append(candidates, strings.Join(parts[len(parts)-size:], "-"))
	}
	return candidates
}

func loadSessionSlug(transcriptPath string) string {
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, `"slug"`) {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if slug := str(entry["slug"]); slug != "" {
			return slug
		}
	}
	return ""
}

func pick(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
