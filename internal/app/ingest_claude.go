package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
	"github.com/chojs23/lazyagent/internal/textutil"
)

func IngestClaudeEvent(ctx context.Context, st *store.Store, payload map[string]any) (IngestResult, error) {
	parsed := claude.ParseRawEvent(payload)
	result := IngestResult{SessionID: parsed.SessionID}

	err := st.WithTx(ctx, func(q *store.Queries) error {
		existingSession, err := q.GetSessionByID(ctx, parsed.SessionID)
		if err != nil {
			return err
		}

		// Skip if session already belongs to a different runtime (e.g. opencode).
		// OpenCode internally uses Claude Code, so Claude hooks fire with the
		// same session ID. Ingesting those events would create duplicates.
		if existingSession != nil && existingSession.Runtime != "" && existingSession.Runtime != "claude" {
			return nil
		}

		var projectID int64
		if existingSession != nil {
			projectID = existingSession.ProjectID
		} else {
			cwd := jsonutil.String(payload["cwd"])
			id, err := resolveProject(ctx, q, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		sessionSlug := ""
		if parsed.Subtype == "UserPromptSubmit" {
			candidate := claudePromptSlug(jsonutil.String(parsed.Metadata["prompt"]))
			if candidate != "" && (existingSession == nil || existingSession.Slug == "" || existingSession.Slug == parsed.Slug) {
				sessionSlug = candidate
			}
		}

		if err := q.UpsertSession(ctx, parsed.SessionID, "", projectID, sessionSlug, "claude", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		rootAgentID := parsed.SessionID
		if err := q.UpsertAgent(ctx, rootAgentID, parsed.SessionID, "", "", "", "", ""); err != nil {
			return err
		}

		if parsed.Subtype == "PreToolUse" && parsed.ToolName == "Agent" {
			toolInput := jsonutil.Map(payload["tool_input"])
			if err := q.AddPendingAgentSpawn(ctx, model.PendingAgentSpawn{
				ToolUseID:    parsed.ToolUseID,
				SessionID:    parsed.SessionID,
				OwnerAgentID: parsed.OwnerAgentID,
				Name:         parsed.SubAgentName,
				Description:  parsed.SubAgentDescription,
				AgentType:    jsonutil.String(toolInput["subagent_type"]),
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

			name := textutil.FirstNonEmpty(agentField(existingAgent, func(a *model.Agent) string { return a.Name }), pendingField(pending, func(p *model.PendingAgentSpawn) string { return p.Name }))
			desc := textutil.FirstNonEmpty(agentField(existingAgent, func(a *model.Agent) string { return a.Description }), pendingField(pending, func(p *model.PendingAgentSpawn) string { return p.Description }))

			if err := q.UpsertAgent(ctx, parsed.OwnerAgentID, parsed.SessionID, rootAgentID, name, desc,
				jsonutil.String(payload["agent_type"]), jsonutil.String(payload["agent_transcript_path"])); err != nil {
				return err
			}
		}

		if parsed.SubAgentID != "" {
			subName := parsed.SubAgentName
			subDesc := parsed.SubAgentDescription
			subType := jsonutil.String(payload["agent_type"])

			if parsed.Subtype == "PostToolUse" && parsed.ToolName == "Agent" && parsed.ToolUseID != "" {
				pending, err := q.TakePendingAgentSpawn(ctx, parsed.ToolUseID)
				if err != nil {
					return err
				}
				if pending != nil {
					subName = textutil.FirstNonEmpty(subName, pending.Name)
					subDesc = textutil.FirstNonEmpty(subDesc, pending.Description)
					subType = textutil.FirstNonEmpty(subType, pending.AgentType)
				}

				toolInput := jsonutil.Map(payload["tool_input"])
				toolResp := jsonutil.Map(payload["tool_response"])
				subType = textutil.FirstNonEmpty(subType,
					jsonutil.String(toolInput["subagent_type"]),
					jsonutil.String(toolResp["agentType"]),
					jsonutil.String(toolResp["subagent_type"]))
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

		if parsed.Subtype == "SubagentStop" && agentID != rootAgentID {
			if err := q.UpdateAgentStatus(ctx, agentID, "stopped"); err != nil {
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

func claudePromptSlug(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if strings.Contains(prompt, "<local-command-caveat>") {
		return ""
	}
	first := strings.TrimSpace(textutil.FirstLine(prompt))
	if first == "" {
		return ""
	}
	if strings.HasPrefix(first, "/") || strings.HasPrefix(first, "<command-name>/") {
		return ""
	}
	return first
}
