package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
)

// ReadPatchEvents reads a Codex JSONL transcript file and returns
// ParsedEvent entries for patch_apply_begin and patch_apply_end events.
// These events are not delivered through the Codex hook system, so we
// read them directly from the transcript.
func ReadPatchEvents(sessionID, transcriptPath string) ([]model.ParsedEvent, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	var events []model.ParsedEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 512*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "event_msg" {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(entry.Payload, &payload); err != nil {
			continue
		}

		payloadType := jsonutil.String(payload["type"])
		if payloadType == "patch_apply_end" {
			ev := parsePatchEnd(sessionID, entry.Timestamp, payload)
			events = append(events, ev)
		}
	}

	return events, scanner.Err()
}

func parsePatchEnd(sessionID, timestamp string, payload map[string]any) model.ParsedEvent {
	callID := jsonutil.String(payload["call_id"])
	changes := jsonutil.Map(payload["changes"])
	stdout := jsonutil.String(payload["stdout"])

	// Build a combined unified diff from all file changes
	combinedDiff := buildUnifiedDiff(changes)

	raw := map[string]any{
		"hook_event_name": "PostToolUse",
		"session_id":      sessionID,
		"tool_name":       "apply_patch",
		"tool_use_id":     callID,
		"tool_response":   stdout,
		"metadata": map[string]any{
			"diff":    combinedDiff,
			"success": payload["success"],
		},
	}

	return model.ParsedEvent{
		SessionID: sessionID,
		Type:      "tool",
		Subtype:   "PostToolUse",
		ToolName:  "apply_patch",
		ToolUseID: callID,
		Timestamp: jsonutil.TimestampMillis(timestamp),
		Metadata:  map[string]any{},
		Raw:       raw,
	}
}

// buildUnifiedDiff creates a standard unified diff from the changes map.
func buildUnifiedDiff(changes map[string]any) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	for filePath, change := range changes {
		cm := jsonutil.Map(change)
		changeType := jsonutil.String(cm["type"])
		switch changeType {
		case "add":
			b.WriteString("--- /dev/null\n")
			b.WriteString("+++ " + filePath + "\n")
			content := jsonutil.String(cm["content"])
			if content != "" {
				for _, line := range strings.Split(content, "\n") {
					b.WriteString("+" + line + "\n")
				}
			}
		case "delete":
			b.WriteString("--- " + filePath + "\n")
			b.WriteString("+++ /dev/null\n")
			content := jsonutil.String(cm["content"])
			if content != "" {
				for _, line := range strings.Split(content, "\n") {
					b.WriteString("-" + line + "\n")
				}
			}
		case "update":
			b.WriteString("--- " + filePath + "\n")
			b.WriteString("+++ " + filePath + "\n")
			diff := jsonutil.String(cm["unified_diff"])
			if diff != "" {
				b.WriteString(diff)
				if !strings.HasSuffix(diff, "\n") {
					b.WriteString("\n")
				}
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
