package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
)

// TokenUsage holds token counts for a single API call.
type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// Add accumulates another TokenUsage into this one.
func (t *TokenUsage) Add(other TokenUsage) {
	t.InputTokens += other.InputTokens
	t.OutputTokens += other.OutputTokens
	t.CacheReadTokens += other.CacheReadTokens
	t.CacheCreationTokens += other.CacheCreationTokens
}

// Total returns the sum of all token categories (input + output + cache read + cache write).
// This is not deduplicated input; it represents total tokens across all billing categories.
func (t TokenUsage) Total() int64 {
	return t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheCreationTokens
}

// ModelStats holds per-model aggregated stats.
type ModelStats struct {
	Calls   int
	Tokens  TokenUsage
	CostUSD float64
}

// ToolStats holds per-tool call count.
type ToolStats struct {
	Calls int
}

// SessionTokenSummary is the aggregated token usage for a session.
type SessionTokenSummary struct {
	Tokens         TokenUsage
	CostUSD        float64
	APICalls       int
	ModelBreakdown map[string]*ModelStats
	ToolBreakdown  map[string]*ToolStats
	BashBreakdown  map[string]*ToolStats
}

// ReadTranscriptTokens reads a Claude Code JSONL transcript file and
// returns aggregated token usage.
func ReadTranscriptTokens(transcriptPath string) (*SessionTokenSummary, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	summary := &SessionTokenSummary{
		ModelBreakdown: make(map[string]*ModelStats),
		ToolBreakdown:  make(map[string]*ToolStats),
		BashBreakdown:  make(map[string]*ToolStats),
	}

	// Track seen message IDs to deduplicate streaming updates.
	// The JSONL file may contain multiple entries for the same assistant
	// message (intermediate streaming states). We keep only the last
	// occurrence of each message ID (which has the final token counts).
	type msgEntry struct {
		model   string
		tokens  TokenUsage
		content []any
	}
	seen := make(map[string]*msgEntry)
	var order []string

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		entryType, _ := entry["type"].(string)
		if entryType != "assistant" {
			continue
		}

		msg, ok := entry["message"].(map[string]any)
		if !ok {
			continue
		}

		usage, ok := msg["usage"].(map[string]any)
		if !ok {
			continue
		}

		msgID := stringVal(msg, "id")
		modelName := normalizeModel(stringVal(msg, "model"))
		if modelName == "" {
			modelName = "unknown"
		}

		tokens := TokenUsage{
			InputTokens:         int64Val(usage, "input_tokens"),
			OutputTokens:        int64Val(usage, "output_tokens"),
			CacheReadTokens:     int64Val(usage, "cache_read_input_tokens"),
			CacheCreationTokens: int64Val(usage, "cache_creation_input_tokens"),
		}

		content, _ := msg["content"].([]any)

		// Deduplicate: keep last occurrence per message ID.
		if msgID == "" {
			msgID = fmt.Sprintf("_no_id_%d", len(order))
		}
		if _, exists := seen[msgID]; !exists {
			order = append(order, msgID)
		}
		seen[msgID] = &msgEntry{
			model:   modelName,
			tokens:  tokens,
			content: content,
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}

	// Second pass: aggregate from deduplicated entries.
	for _, id := range order {
		e := seen[id]
		cost := CalculateCost(e.model, e.tokens)

		summary.Tokens.Add(e.tokens)
		summary.CostUSD += cost
		summary.APICalls++

		ms, ok := summary.ModelBreakdown[e.model]
		if !ok {
			ms = &ModelStats{}
			summary.ModelBreakdown[e.model] = ms
		}
		ms.Calls++
		ms.Tokens.Add(e.tokens)
		ms.CostUSD += cost

		for _, block := range e.content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := b["type"].(string)
			if blockType != "tool_use" {
				continue
			}

			toolName, _ := b["name"].(string)
			if toolName == "" {
				continue
			}

			if !strings.HasPrefix(toolName, "mcp__") {
				ts, ok := summary.ToolBreakdown[toolName]
				if !ok {
					ts = &ToolStats{}
					summary.ToolBreakdown[toolName] = ts
				}
				ts.Calls++
			}

			if toolName == "Bash" || toolName == "BashTool" {
				input, _ := b["input"].(map[string]any)
				cmd, _ := input["command"].(string)
				for _, name := range ExtractCommandNames(cmd) {
					bs, ok := summary.BashBreakdown[name]
					if !ok {
						bs = &ToolStats{}
						summary.BashBreakdown[name] = bs
					}
					bs.Calls++
				}
			}
		}
	}

	return summary, nil
}

// ExtractCommandNames splits a shell command string on separators
// (&&, ;, |) and returns the base command name of each segment.
func ExtractCommandNames(command string) []string {
	if strings.TrimSpace(command) == "" {
		return nil
	}

	// Split on common shell separators.
	parts := splitOnSeparators(command)
	var names []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens := strings.Fields(part)
		// Skip leading environment variable assignments (FOO=bar).
		i := 0
		for i < len(tokens) && strings.Contains(tokens[i], "=") && !strings.HasPrefix(tokens[i], "-") {
			i++
		}
		if i >= len(tokens) {
			continue
		}
		base := path.Base(tokens[i])
		if base == "" || base == "cd" || base == "true" || base == "false" {
			continue
		}
		names = append(names, base)
	}
	return names
}

func splitOnSeparators(s string) []string {
	var result []string
	inQuote := byte(0)
	start := 0
	i := 0
	for i < len(s) {
		ch := s[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
			} else if ch == '\\' && i+1 < len(s) {
				i++ // skip escaped char
			}
			i++
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			i++
			continue
		}
		if ch == '|' || ch == ';' {
			result = append(result, s[start:i])
			i++
			start = i
			continue
		}
		if ch == '&' && i+1 < len(s) && s[i+1] == '&' {
			result = append(result, s[start:i])
			i += 2
			start = i
			continue
		}
		i++
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func stringVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
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
