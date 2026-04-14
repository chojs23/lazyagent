package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/codex"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/opencode"
	"github.com/chojs23/lazyagent/internal/store"
)

type IngestResult struct {
	EventID   int64  `json:"event_id"`
	SessionID string `json:"session_id"`
	ProjectID int64  `json:"project_id"`
}

const maxProjectSlugSuffix = 1000

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
			cwd := str(payload["cwd"])
			id, err := resolveProject(ctx, q, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		sessionSlug := ""
		if parsed.Subtype == "UserPromptSubmit" {
			candidate := claudePromptSlug(str(parsed.Metadata["prompt"]))
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

		// Mark subagent as stopped when it finishes
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
			cwd := str(payload["cwd"])
			if cwd == "" {
				cwd = str(payload["project_dir"])
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
		title := str(payload["title"])
		sessionSlug := title
		if err := q.UpsertSession(ctx, parsed.SessionID, parentSessionID, projectID, sessionSlug, "opencode", parsed.Metadata, parsed.Timestamp, parsed.TranscriptPath); err != nil {
			return err
		}

		// Derive agent name only from definitive sources:
		// - SubAgentName: explicitly parsed from title's @subagent pattern (child sessions)
		// - title on SessionStart: the initial session title is the canonical name
		// - title on session.updated: OpenCode may replace a placeholder title
		//   (e.g. "New session - <timestamp>") with the real title after the
		//   first user prompt is submitted
		// - agent_name on message.updated: OpenCode includes the agent name
		//   (e.g. "Main agent") on assistant messages, which is
		//   the actual agent the user selected rather than the generic "main"
		// - "main" default: only for new root sessions on SessionStart
		// All other events pass empty so nullIfEmpty + COALESCE in UpsertAgent
		// preserves the previously stored name. This prevents tool output
		// summaries (PostToolUse title) from overwriting the agent name.
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
			// Only update agent name from title for child sessions.
			// Root sessions keep "main" as their agent name; their title
			// (derived from the user's first prompt) is stored as the
			// session slug for sidebar display instead.
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
			// Mirror session stop to its root agent
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
			// Mirror session reactivation to its root agent
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
//
// Upstream emits a user prompt as:
//  1. `message.updated` with `message_role = user`
//  2. one or more later `message.part.updated` events carrying the parts
//
// We treat only the first text part for that message as the real prompt body.
// Later text parts stay as `PartUpdated` so synthetic attachment-expansion text
// does not create duplicate user turns.
func normalizeOpenCodeUserPrompt(ctx context.Context, q *store.Queries, parsed *model.ParsedEvent) error {
	if parsed.Subtype != "PartUpdated" {
		return nil
	}
	if str(parsed.Metadata["part_type"]) != "text" {
		return nil
	}

	messageID := str(parsed.Metadata["message_id"])
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

	prompt := str(parsed.Metadata["text"])
	parsed.Type = "user"
	parsed.Subtype = "UserPromptSubmit"
	parsed.Metadata["prompt"] = prompt
	parsed.Metadata["message_role"] = "user"
	parsed.Raw["prompt"] = prompt
	parsed.Raw["message_role"] = "user"
	parsed.Raw["message_id"] = messageID
	return nil
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
	if err := q.UpdateAgentStatus(ctx, sessionID, "stopped"); err != nil {
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
			cwd := str(payload["cwd"])
			id, err := resolveProject(ctx, q, parsed.TranscriptPath, cwd)
			if err != nil {
				return err
			}
			projectID = id
		}

		// Codex hook payloads do not carry a dedicated session title, but the
		// first user prompt is still a good human-readable label. Persist only the
		// first non-empty prompt we see so later follow-up prompts do not rename
		// the session out from under the user.
		sessionSlug := ""
		if parsed.Subtype == "UserPromptSubmit" && (existingSession == nil || existingSession.Slug == "") {
			sessionSlug = codexPromptSlug(str(parsed.Metadata["prompt"]))
		}

		// Read parent-child relationship from the Codex session file.
		// The hook payload does not include parent info, so we read the
		// first line of the JSONL transcript file which contains
		// forked_from_id for subagent sessions.
		// Lazy approach: only read if the session doesn't already have
		// a parent set, since the file may not exist at SessionStart time.
		//
		// When the parent is already known, pass empty agentName/agentDesc
		// so that nullIfEmpty produces NULL and COALESCE in UpsertAgent
		// preserves the previously stored nickname (e.g. "Cicero").
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
		// Codex agents should not inherit the default 'claude-code' agent_class.
		if err := q.UpdateAgentClass(ctx, rootAgentID, "codex"); err != nil {
			return err
		}

		// Session lifecycle: Option B
		// Stop marks the session stopped; any subsequent activity reactivates it.
		// This mirrors the OpenCode pattern and avoids needing a stale-session
		// cleanup mechanism.
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

		// Codex does not fire hooks for apply_patch events. Scan the
		// transcript file and ingest any patch events not yet stored.
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

func resolveProject(ctx context.Context, q *store.Queries, transcriptPath, cwd string) (int64, error) {
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

		// OpenCode often provides a working directory without a transcript path.
		// In that case, derive a stable slug from the directory so brand-new
		// projects still get a meaningful project entry instead of falling back to
		// `unknown`.
		candidates := deriveSlugCandidates(cwd)
		for _, c := range candidates {
			proj, err := q.GetProjectBySlug(ctx, c)
			if err != nil {
				return 0, err
			}
			if proj != nil {
				if proj.Directory == "" {
					q.UpdateProjectDirectory(ctx, proj.ID, cwd)
				}
				return proj.ID, nil
			}
		}

		for _, c := range candidates {
			avail, err := q.IsSlugAvailable(ctx, c)
			if err != nil {
				return 0, err
			}
			if avail {
				return q.CreateProject(ctx, c, c, cwd, "")
			}
		}

		base := candidates[0]
		return createProjectWithUniqueSlug(ctx, q, base, cwd, "")
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
		return createProjectWithUniqueSlug(ctx, q, base, cwd, transcriptDir)
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

func createProjectWithUniqueSlug(ctx context.Context, q *store.Queries, base, directory, transcriptPath string) (int64, error) {
	for suffix := 2; suffix <= maxProjectSlugSuffix; suffix++ {
		slug := fmt.Sprintf("%s-%d", base, suffix)
		avail, err := q.IsSlugAvailable(ctx, slug)
		if err != nil {
			return 0, err
		}
		if avail {
			return q.CreateProject(ctx, slug, slug, directory, transcriptPath)
		}
	}

	return 0, fmt.Errorf("resolve project slug: exhausted suffixes for %q up to %d", base, maxProjectSlugSuffix)
}

func extractProjectDir(transcriptPath string) string {
	cleaned := strings.TrimRight(transcriptPath, "/")
	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		return cleaned
	}
	if ext := filepath.Ext(cleaned); ext != "" {
		return filepath.Dir(cleaned)
	}
	return cleaned
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

func claudePromptSlug(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if strings.Contains(prompt, "<local-command-caveat>") {
		return ""
	}
	first := strings.TrimSpace(firstLineText(prompt))
	if first == "" {
		return ""
	}
	if strings.HasPrefix(first, "/") || strings.HasPrefix(first, "<command-name>/") {
		return ""
	}
	return first
}

func firstLineText(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
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
