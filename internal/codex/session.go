package codex

import (
	"bufio"
	"encoding/json"
	"os"
)

// SessionMeta holds parent-child relationship data extracted from the first
// line of a Codex session JSONL file. Codex hook payloads do not include
// parent session information, so we read it from the session file directly.
type SessionMeta struct {
	ParentSessionID string
	AgentNickname   string
	AgentRole       string
}

// ReadSessionMeta reads the first line of a Codex session JSONL file and
// extracts parent-child relationship fields. Returns an empty SessionMeta
// (not an error) if the file does not exist, is empty, or is not a
// subagent session.
func ReadSessionMeta(path string) SessionMeta {
	if path == "" {
		return SessionMeta{}
	}

	f, err := os.Open(path)
	if err != nil {
		return SessionMeta{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// The first line can be large (base_instructions, etc.), so allow up to 1MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return SessionMeta{}
	}

	var line struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return SessionMeta{}
	}
	if line.Type != "session_meta" {
		return SessionMeta{}
	}

	var payload struct {
		ForkedFromID  string `json:"forked_from_id"`
		AgentNickname string `json:"agent_nickname"`
		AgentRole     string `json:"agent_role"`
	}
	if err := json.Unmarshal(line.Payload, &payload); err != nil {
		return SessionMeta{}
	}

	parentID := payload.ForkedFromID

	// Fallback: older Codex versions may omit forked_from_id but still
	// include the parent thread ID nested inside source.subagent.thread_spawn.
	// Also extract agent_nickname and agent_role from thread_spawn if the
	// top-level fields are absent.
	if parentID == "" || payload.AgentNickname == "" || payload.AgentRole == "" {
		var extra struct {
			Source json.RawMessage `json:"source"`
		}
		if json.Unmarshal(line.Payload, &extra) == nil && len(extra.Source) > 0 {
			ts := extractThreadSpawn(extra.Source)
			if parentID == "" {
				parentID = ts.parentThreadID
			}
			if payload.AgentNickname == "" {
				payload.AgentNickname = ts.agentNickname
			}
			if payload.AgentRole == "" {
				payload.AgentRole = ts.agentRole
			}
		}
	}

	return SessionMeta{
		ParentSessionID: parentID,
		AgentNickname:   payload.AgentNickname,
		AgentRole:       payload.AgentRole,
	}
}

type threadSpawnInfo struct {
	parentThreadID string
	agentNickname  string
	agentRole      string
}

// extractThreadSpawn parses source.subagent.thread_spawn from the raw
// source JSON. The source field can be a plain string (e.g. "cli") or
// an object like {"subagent": {"thread_spawn": {...}}}.
func extractThreadSpawn(sourceRaw json.RawMessage) threadSpawnInfo {
	var wrapper struct {
		Subagent json.RawMessage `json:"subagent"`
	}
	if json.Unmarshal(sourceRaw, &wrapper) != nil || len(wrapper.Subagent) == 0 {
		return threadSpawnInfo{}
	}

	var inner struct {
		ThreadSpawn struct {
			ParentThreadID string `json:"parent_thread_id"`
			AgentNickname  string `json:"agent_nickname"`
			AgentRole      string `json:"agent_role"`
		} `json:"thread_spawn"`
	}
	if json.Unmarshal(wrapper.Subagent, &inner) != nil {
		return threadSpawnInfo{}
	}

	return threadSpawnInfo{
		parentThreadID: inner.ThreadSpawn.ParentThreadID,
		agentNickname:  inner.ThreadSpawn.AgentNickname,
		agentRole:      inner.ThreadSpawn.AgentRole,
	}
}
