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

	var accountedTokens claude.TokenUsage
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
				applyCodexTokenCount(summary, jsonutil.Map(payload["info"]), modelName, &accountedTokens)
			}

		case "response_item":
			itemType := jsonutil.String(payload["type"])
			if itemType == "function_call" {
				toolName := jsonutil.String(payload["name"])
				if toolName == "" {
					continue
				}

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
					for _, name := range claude.ExtractCommandNames(cmd) {
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

	if summary.Tokens.Total() == 0 && summary.APICalls == 0 {
		return nil, nil
	}

	return summary, nil
}

func applyCodexTokenCount(summary *claude.SessionTokenSummary, info map[string]any, modelName string, accountedTokens *claude.TokenUsage) {
	if len(info) == 0 {
		return
	}

	if total := readCodexTokenUsage(jsonutil.Map(info["total_token_usage"])); total.Total() > 0 {
		delta := codexTokenDelta(*accountedTokens, total)
		if delta.Total() > 0 {
			addCodexTokenUsage(summary, modelName, delta)
			accountedTokens.Add(delta)
		}
		return
	}

	if last := readCodexTokenUsage(jsonutil.Map(info["last_token_usage"])); last.Total() > 0 {
		addCodexTokenUsage(summary, modelName, last)
		accountedTokens.Add(last)
	}
}

func addCodexTokenUsage(summary *claude.SessionTokenSummary, modelName string, tokens claude.TokenUsage) {
	if modelName == "" {
		modelName = "unknown"
	}

	cost := claude.CalculateCost(modelName, tokens)
	summary.Tokens.Add(tokens)
	summary.CostUSD += cost
	summary.APICalls++

	ms, ok := summary.ModelBreakdown[modelName]
	if !ok {
		ms = &claude.ModelStats{}
		summary.ModelBreakdown[modelName] = ms
	}
	ms.Calls++
	ms.Tokens.Add(tokens)
	ms.CostUSD += cost
}

func readCodexTokenUsage(m map[string]any) claude.TokenUsage {
	if len(m) == 0 {
		return claude.TokenUsage{}
	}
	return claude.TokenUsage{
		InputTokens:     int64Val(m, "input_tokens"),
		OutputTokens:    int64Val(m, "output_tokens"),
		CacheReadTokens: int64Val(m, "cached_input_tokens"),
	}
}

func codexTokenDelta(previous, current claude.TokenUsage) claude.TokenUsage {
	return claude.TokenUsage{
		InputTokens:     nonNegativeDelta(previous.InputTokens, current.InputTokens),
		OutputTokens:    nonNegativeDelta(previous.OutputTokens, current.OutputTokens),
		CacheReadTokens: nonNegativeDelta(previous.CacheReadTokens, current.CacheReadTokens),
	}
}

func nonNegativeDelta(previous, current int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
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
