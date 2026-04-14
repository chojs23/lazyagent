package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/codex"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

func IngestCodexEvent(ctx context.Context, st *store.Store, payload map[string]any) (IngestResult, error) {
	parsed := codex.ParseRawEvent(payload)
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
			cwd := jsonutil.String(payload["cwd"])
			id, err := resolveProject(ctx, q, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		sessionSlug := ""
		if parsed.Subtype == "UserPromptSubmit" && (existingSession == nil || existingSession.Slug == "") {
			sessionSlug = codexPromptSlug(jsonutil.String(parsed.Metadata["prompt"]))
		}

		parentSessionID := ""
		agentName := ""
		agentDesc := ""
		if existingSession == nil || existingSession.ParentSessionID == "" {
			meta := codex.ReadSessionMeta(parsed.TranscriptPath)
			parentSessionID = meta.ParentSessionID
			agentName = meta.AgentNickname
			if meta.AgentRole != "" {
				if agentName != "" {
					agentName = agentName + " (" + meta.AgentRole + ")"
				} else {
					agentName = meta.AgentRole
				}
			}
			agentDesc = meta.AgentRole
			if agentName == "" && existingSession == nil {
				agentName = "codex"
			}
		}

		if err := q.UpsertSession(ctx, parsed.SessionID, parentSessionID, projectID, sessionSlug, "codex", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		rootAgentID := parsed.SessionID
		if err := q.UpsertAgent(ctx, rootAgentID, parsed.SessionID, "", agentName, agentDesc, "", ""); err != nil {
			return err
		}
		if err := q.UpdateAgentClass(ctx, rootAgentID, "codex"); err != nil {
			return err
		}

		if parsed.Subtype == "Stop" {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "stopped"); err != nil {
				return err
			}
			if err := q.UpdateAgentStatus(ctx, rootAgentID, "stopped"); err != nil {
				return err
			}
		} else if existingSession != nil && existingSession.Status == "stopped" {
			if err := q.UpdateSessionStatus(ctx, parsed.SessionID, "active"); err != nil {
				return err
			}
			if err := q.UpdateAgentStatus(ctx, rootAgentID, "active"); err != nil {
				return err
			}
		}

		raw, err := json.Marshal(parsed.Raw)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}

		eventID, err := q.InsertEvent(ctx, model.Event{
			AgentID:   rootAgentID,
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

		if parsed.TranscriptPath != "" {
			if err := ingestCodexPatchEvents(ctx, q, parsed.SessionID, parsed.TranscriptPath, rootAgentID); err != nil {
				return err
			}
		}

		return nil
	})

	return result, err
}

func ingestCodexPatchEvents(ctx context.Context, q *store.Queries, sessionID, transcriptPath, rootAgentID string) error {
	patchEvents, err := codex.ReadPatchEvents(sessionID, transcriptPath)
	if err != nil {
		applog.Error("Read Codex patch events failed", fmt.Errorf("session %s transcript %s: %w", sessionID, transcriptPath, err))
		return nil
	}

	for _, pe := range patchEvents {
		if pe.ToolUseID == "" {
			continue
		}

		exists, err := q.EventExistsByToolUseID(ctx, sessionID, pe.ToolUseID)
		if err != nil {
			err = fmt.Errorf("check Codex patch event %q: %w", pe.ToolUseID, err)
			applog.Error("Codex patch dedupe failed", err)
			return err
		}
		if exists {
			continue
		}

		pRaw, err := json.Marshal(pe.Raw)
		if err != nil {
			err = fmt.Errorf("encode Codex patch event %q: %w", pe.ToolUseID, err)
			applog.Error("Codex patch encode failed", err)
			return err
		}

		if _, err := q.InsertEvent(ctx, model.Event{
			AgentID:   rootAgentID,
			SessionID: sessionID,
			Type:      pe.Type,
			Subtype:   pe.Subtype,
			ToolName:  pe.ToolName,
			ToolUseID: pe.ToolUseID,
			Timestamp: pe.Timestamp,
			Payload:   string(pRaw),
		}); err != nil {
			err = fmt.Errorf("insert Codex patch event %q: %w", pe.ToolUseID, err)
			applog.Error("Codex patch insert failed", err)
			return err
		}
	}

	return nil
}

func codexPromptSlug(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if idx := strings.IndexByte(prompt, '\n'); idx >= 0 {
		prompt = prompt[:idx]
	}
	return strings.TrimSpace(prompt)
}
