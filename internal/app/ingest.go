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
			cwd := str(payload["cwd"])
			id, err := resolveProject(ctx, q, projectSlugOverride, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		if err := q.UpsertSession(ctx, parsed.SessionID, "", projectID, parsed.Slug, "claude", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
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
			cwd := str(payload["cwd"])
			if cwd == "" {
				cwd = str(payload["project_dir"])
			}
			id, err := resolveProject(ctx, q, slug, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		parentSessionID, _ := parsed.Metadata["parent_session_id"].(string)
		if parentSessionID == "" && existingSession != nil {
			parentSessionID = existingSession.ParentSessionID
		}
		title := str(payload["title"])
		slug := title
		if err := q.UpsertSession(ctx, parsed.SessionID, parentSessionID, projectID, slug, "opencode", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		// Derive agent name only from definitive sources:
		// - SubAgentName: explicitly parsed from title's @subagent pattern (child sessions)
		// - title on SessionStart: the initial session title is the canonical name
		// - title on session.updated: OpenCode may replace a placeholder title
		//   (e.g. "New session - <timestamp>") with the real title after the
		//   first user prompt is submitted
		// - "main" default: only for new root sessions on SessionStart
		// All other events pass empty so nullIfEmpty + COALESCE in UpsertAgent
		// preserves the previously stored name. This prevents tool output
		// summaries (PostToolUse title) from overwriting the agent name.
		rootAgentID := parsed.SessionID
		agentName := ""
		if parsed.SubAgentName != "" {
			agentName = parsed.SubAgentName
		} else if parsed.Subtype == "SessionStart" {
			if title != "" {
				agentName = title
			} else if parentSessionID == "" {
				agentName = "main"
			}
		} else if parsed.Subtype == "session.updated" && title != "" {
			agentName = title
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

		// OpenCode uses `session.status` with status_type "idle"/"busy"/"retry"
		// as the canonical execution state signal. The older `session.idle` event
		// (mapped to subtype "Stop") is deprecated but still supported.
		//
		// A parent session may go idle while its child sessions (subagents)
		// are still running. In that case we defer stopping the parent until the
		// last child finishes, then cascade the stop upward.
		//
		// OpenCode emits passive follow-up events (session.updated, session.diff)
		// after idle. Only real activity signals should reactivate a stopped session.
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
			if parentSessionID != "" {
				if err := cascadeParentStop(ctx, q, parentSessionID); err != nil {
					return err
				}
			}
		} else if existingSession != nil && existingSession.Status == "stopped" && isOpenCodeResumeSignal(parsed) {
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

// cascadeParentStop checks whether a parent session should be stopped after one
// of its children stopped. The parent is stopped only when it has no remaining
// active children AND it already received its own idle signal (either the
// deprecated session.idle "Stop" event or the newer session.status "idle").
// The check recurses upward so multi-level subagent trees are handled.
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
	session, err := q.GetSessionByID(ctx, sessionID)
	if err != nil || session == nil || session.ParentSessionID == "" {
		return err
	}
	return cascadeParentStop(ctx, q, session.ParentSessionID)
}

// isOpenCodeResumeSignal returns true for events that indicate real session
// activity, as opposed to passive follow-ups (session.updated, session.diff)
// that OpenCode emits after idle.
func isOpenCodeResumeSignal(parsed model.ParsedEvent) bool {
	// session.status with type "busy" means the session is actively working
	if parsed.Subtype == "SessionStatus" {
		statusType, _ := parsed.Metadata["status_type"].(string)
		return statusType == "busy"
	}
	switch str(parsed.Raw["event"]) {
	case "tool.execute.before", "tool.execute.after", "session.created", "permission.asked":
		return true
	default:
		return false
	}
}

func resolveProject(ctx context.Context, q *store.Queries, slugOverride, transcriptPath, cwd string) (int64, error) {
	// Try matching by working directory first. This is the most reliable
	// cross-runtime match: both Claude and OpenCode provide the actual
	// project directory (cwd / project_dir), while their transcript paths
	// use completely different formats.
	if cwd != "" {
		proj, err := q.GetProjectByDirectory(ctx, cwd)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			return proj.ID, nil
		}
	}

	if slugOverride != "" {
		proj, err := q.GetProjectBySlug(ctx, slugOverride)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			if cwd != "" && proj.Directory == "" {
				q.UpdateProjectDirectory(ctx, proj.ID, cwd)
			}
			return proj.ID, nil
		}
		transcriptDir := ""
		if transcriptPath != "" {
			transcriptDir = extractProjectDir(transcriptPath)
		}
		return q.CreateProject(ctx, slugOverride, slugOverride, cwd, transcriptDir)
	}

	if transcriptPath != "" {
		transcriptDir := extractProjectDir(transcriptPath)
		proj, err := q.GetProjectByTranscriptPath(ctx, transcriptDir)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			if cwd != "" && proj.Directory == "" {
				q.UpdateProjectDirectory(ctx, proj.ID, cwd)
			}
			return proj.ID, nil
		}

		candidates := deriveSlugCandidates(transcriptPath)
		for _, c := range candidates {
			avail, err := q.IsSlugAvailable(ctx, c)
			if err != nil {
				return 0, err
			}
			if avail {
				return q.CreateProject(ctx, c, c, cwd, transcriptDir)
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
				return q.CreateProject(ctx, c, c, cwd, transcriptDir)
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
	return q.CreateProject(ctx, "unknown", "unknown", "", "")
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
	seen := make(map[string]struct{})
	addCandidate := func(candidate string) {
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	// Claude transcript directories encode the full project path into a single
	// hyphen-separated segment. In practice the trailing repo slug is usually the
	// last two tokens, so prefer that before falling back to shorter or broader
	// suffixes during slug availability checks.
	addCandidate(strings.Join(parts[max(0, len(parts)-2):], "-"))
	addCandidate(parts[len(parts)-1])
	for start := len(parts) - 3; start >= 0; start-- {
		addCandidate(strings.Join(parts[start:], "-"))
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
