package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/opencode"
	"github.com/chojs23/lazyagent/internal/store"
)

func IngestOpenCodeEvent(ctx context.Context, st *store.Store, payload map[string]any) (IngestResult, error) {
	parsed := opencode.ParseRawEvent(payload)
	result := IngestResult{SessionID: parsed.SessionID}

	err := st.WithTx(ctx, func(q *store.Queries) error {
		if err := normalizeOpenCodeUserPrompt(ctx, q, &parsed); err != nil {
			return err
		}

		existingSession, err := q.GetSessionByID(ctx, parsed.SessionID)
		if err != nil {
			return err
		}

		var projectID int64
		if existingSession != nil {
			projectID = existingSession.ProjectID
		} else {
			cwd := jsonutil.String(payload["cwd"])
			if cwd == "" {
				cwd = jsonutil.String(payload["project_dir"])
			}
			id, err := resolveProject(ctx, q, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		parentSessionID, _ := parsed.Metadata["parent_session_id"].(string)
		if parentSessionID == "" && existingSession != nil {
			parentSessionID = existingSession.ParentSessionID
		}
		title := jsonutil.String(payload["title"])
		sessionSlug := title
		if err := q.UpsertSession(ctx, parsed.SessionID, parentSessionID, projectID, sessionSlug, "opencode", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		rootAgentID := parsed.SessionID
		agentName := ""
		if parsed.SubAgentName != "" {
			agentName = parsed.SubAgentName
		} else if parsed.Subtype == "SessionStart" {
			if parentSessionID == "" {
				agentName = "opencode"
			} else if title != "" {
				agentName = title
			}
		} else if parsed.Subtype == "SessionUpdated" && title != "" && parentSessionID != "" {
			agentName = title
		} else if parsed.Subtype == "MessageUpdated" {
			if name, _ := parsed.Metadata["agent_name"].(string); name != "" {
				agentName = name
			}
		}
		if err := q.UpsertAgent(ctx, rootAgentID, parsed.SessionID, "", agentName, parsed.SubAgentDescription, "", ""); err != nil {
			return err
		}

		agentID := rootAgentID
		if parsed.OwnerAgentID != "" && parsed.OwnerAgentID != rootAgentID {
			agentID = parsed.OwnerAgentID
			if err := q.UpsertAgent(ctx, agentID, parsed.SessionID, rootAgentID, "", "", "", ""); err != nil {
				return err
			}
		}

		statusType, _ := parsed.Metadata["status_type"].(string)
		isIdleStatus := parsed.Subtype == "SessionStatus" && statusType == "idle"

		shouldStop := false
		if parsed.Subtype == "SessionEnd" || parsed.Subtype == "StopFailure" {
			shouldStop = true
		} else if parsed.Subtype == "Stop" || isIdleStatus {
			hasActive, err := q.HasActiveChildSessions(ctx, parsed.SessionID)
			if err != nil {
				return err
			}
			shouldStop = !hasActive
		}

		if shouldStop {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "stopped"); err != nil {
				return err
			}
			if err := q.UpdateAgentStatus(ctx, parsed.SessionID, "stopped"); err != nil {
				return err
			}
			if parentSessionID != "" {
				if err := cascadeParentStop(ctx, q, parentSessionID); err != nil {
					return err
				}
			}
		} else if existingSession != nil && existingSession.Status == "stopped" && isOpenCodeResumeSignal(parsed) {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "active"); err != nil {
				return err
			}
			if err := q.UpdateAgentStatus(ctx, parsed.SessionID, "active"); err != nil {
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

// normalizeOpenCodeUserPrompt folds OpenCode's split user-message model into
// the existing `UserPromptSubmit` event shape used everywhere else.
func normalizeOpenCodeUserPrompt(ctx context.Context, q *store.Queries, parsed *model.ParsedEvent) error {
	if parsed.Subtype != "PartUpdated" {
		return nil
	}
	if jsonutil.String(parsed.Metadata["part_type"]) != "text" {
		return nil
	}

	messageID := jsonutil.String(parsed.Metadata["message_id"])
	if messageID == "" {
		return nil
	}

	hasUserMessage, err := q.HasOpenCodeUserMessage(ctx, parsed.SessionID, messageID)
	if err != nil {
		return fmt.Errorf("find opencode user message: %w", err)
	}
	if !hasUserMessage {
		return nil
	}

	alreadyNormalized, err := q.HasOpenCodeNormalizedUserPrompt(ctx, parsed.SessionID, messageID)
	if err != nil {
		return fmt.Errorf("find normalized opencode prompt: %w", err)
	}
	if alreadyNormalized {
		return nil
	}

	prompt := jsonutil.String(parsed.Metadata["text"])
	parsed.Type = "user"
	parsed.Subtype = "UserPromptSubmit"
	parsed.Metadata["prompt"] = prompt
	parsed.Metadata["message_role"] = "user"
	parsed.Raw["prompt"] = prompt
	parsed.Raw["message_role"] = "user"
	parsed.Raw["message_id"] = messageID
	return nil
}

func cascadeParentStop(ctx context.Context, q *store.Queries, sessionID string) error {
	hasActive, err := q.HasActiveChildSessions(ctx, sessionID)
	if err != nil || hasActive {
		return err
	}
	received, err := q.HasSessionIdleEvent(ctx, sessionID)
	if err != nil || !received {
		return err
	}
	if err := q.UpdateSessionStatus(ctx, sessionID, "stopped"); err != nil {
		return err
	}
	if err := q.UpdateAgentStatus(ctx, sessionID, "stopped"); err != nil {
		return err
	}
	session, err := q.GetSessionByID(ctx, sessionID)
	if err != nil || session == nil || session.ParentSessionID == "" {
		return err
	}
	return cascadeParentStop(ctx, q, session.ParentSessionID)
}

func isOpenCodeResumeSignal(parsed model.ParsedEvent) bool {
	if parsed.Subtype == "SessionStatus" {
		statusType, _ := parsed.Metadata["status_type"].(string)
		return statusType == "busy"
	}
	switch jsonutil.String(parsed.Raw["event"]) {
	case "tool.execute.before", "tool.execute.after", "session.created", "permission.asked":
		return true
	default:
		return false
	}
}
