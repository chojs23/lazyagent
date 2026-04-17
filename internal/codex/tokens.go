package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/jsonutil"
)

// ReadTranscriptTokens reads a Codex JSONL transcript file and returns
// aggregated token usage. Codex stores cumulative token counts in
// token_count events, tool usage in response_item/function_call events,
// and model info in turn_context events.
func ReadTranscriptTokens(transcriptPath string) (*claude.SessionTokenSummary, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	summary := &claude.SessionTokenSummary{
		ModelBreakdown: make(map[string]*claude.ModelStats),
		ToolBreakdown:  make(map[string]*claude.ToolStats),
		BashBreakdown:  make(map[string]*claude.ToolStats),
	}

	var lastTotalUsage map[string]any
	modelName := "unknown"

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		entryType := jsonutil.String(entry["type"])
		payload := jsonutil.MapOrEmpty(entry["payload"])

		switch entryType {
		case "turn_context":
			if m := jsonutil.String(payload["model"]); m != "" {
				modelName = m
			}

		case "event_msg":
			payloadType := jsonutil.String(payload["type"])
			if payloadType == "token_count" {
				info := jsonutil.Map(payload["info"])
				if info != nil {
					if total := jsonutil.Map(info["total_token_usage"]); total != nil {
						lastTotalUsage = total
					}
				}
			}

		case "response_item":
			itemType := jsonutil.String(payload["type"])
			if itemType == "function_call" {
				toolName := jsonutil.String(payload["name"])
				if toolName == "" {
					continue
				}
				summary.APICalls++

				if !strings.HasPrefix(toolName, "mcp__") {
					ts, ok := summary.ToolBreakdown[toolName]
					if !ok {
						ts = &claude.ToolStats{}
						summary.ToolBreakdown[toolName] = ts
					}
					ts.Calls++
				}

				// Extract shell commands from shell_command tool.
				if toolName == "shell_command" {
					args := jsonutil.String(payload["arguments"])
					cmd := extractCodexCommand(args)
					for _, name := range extractCodexCommandNames(cmd) {
						bs, ok := summary.BashBreakdown[name]
						if !ok {
							bs = &claude.ToolStats{}
							summary.BashBreakdown[name] = bs
						}
						bs.Calls++
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}

	// Use the last cumulative token_count for totals.
	if lastTotalUsage != nil {
		summary.Tokens = claude.TokenUsage{
			InputTokens:     int64Val(lastTotalUsage, "input_tokens"),
			OutputTokens:    int64Val(lastTotalUsage, "output_tokens"),
			CacheReadTokens: int64Val(lastTotalUsage, "cached_input_tokens"),
		}

		cost := claude.CalculateCost(modelName, summary.Tokens)
		summary.CostUSD = cost

		ms := &claude.ModelStats{
			Calls:   1,
			Tokens:  summary.Tokens,
			CostUSD: cost,
		}
		summary.ModelBreakdown[modelName] = ms
	}

	if summary.Tokens.Total() == 0 && summary.APICalls == 0 {
		return nil, nil
	}

	return summary, nil
}

// extractCodexCommand extracts the command string from a shell_command
// tool's JSON arguments field.
func extractCodexCommand(args string) string {
	if args == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	// Codex uses "command" or "cmd" field.
	cmd := jsonutil.String(parsed["command"])
	if cmd == "" {
		cmd = jsonutil.String(parsed["cmd"])
	}
	return cmd
}

func extractCodexCommandNames(command string) []string {
	return claude.ExtractCommandNames(command)
}

func int64Val(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}

