// Package tokens provides session token-usage aggregation that is shared
// between the TUI's overlay and the web UI's usage modal.
//
// Three sources are supported:
//   - Claude transcript JSONL    (claude.ReadTranscriptTokens)
//   - Codex transcript JSONL     (codex.ReadTranscriptTokens)
//   - Recorded events in the DB  (FromEvents — used as the OpenCode default
//     and as a fallback for runtimes without a transcript path)
//
// All three return a *claude.SessionTokenSummary so downstream renderers
// only need to handle one shape.
package tokens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/codex"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

// EventPageSize is the number of events fetched per DB round-trip while
// aggregating from the events table. Exposed so callers can tune it for
// tests or memory-constrained environments.
var EventPageSize = 2000

// For dispatches token aggregation by session runtime. Returns nil when no
// usage information is available (the caller is expected to surface a
// "no token data" message in that case).
func For(ctx context.Context, st *store.Store, session *model.Session) (*claude.SessionTokenSummary, error) {
	if session == nil {
		return nil, fmt.Errorf("nil session")
	}
	switch session.Runtime {
	case "claude":
		if session.TranscriptPath == "" {
			return nil, fmt.Errorf("no transcript path")
		}
		return claude.ReadTranscriptTokens(session.TranscriptPath)
	case "codex":
		if session.TranscriptPath == "" {
			return nil, fmt.Errorf("no transcript path")
		}
		return codex.ReadTranscriptTokens(session.TranscriptPath)
	default:
		if st == nil {
			return nil, fmt.Errorf("no store")
		}
		return FromEvents(ctx, st.Read(), session.ID)
	}
}

// FromEvents builds a token summary by paging through events stored for
// the given session tree. Used for OpenCode and any runtime that does not
// produce a separate transcript file.
func FromEvents(ctx context.Context, q *store.Queries, sessionID string) (*claude.SessionTokenSummary, error) {
	agg := newAggregator()
	offset := 0

	for {
		events, err := q.ListEventsForSessionTree(ctx, sessionID, model.EventFilter{
			Limit:  EventPageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		for _, ev := range events {
			agg.consume(ev)
		}
		offset += len(events)
		if len(events) < EventPageSize {
			break
		}
	}

	return agg.finalize(), nil
}

// aggregator deduplicates per-message token counts (multiple
// `MessageUpdated` events can carry intermediate streaming states for the
// same message id; only the final one should be counted) and tallies
// tool / Bash command usage by walking PreToolUse events.
type aggregator struct {
	summary  *claude.SessionTokenSummary
	messages map[string]eventTokenMessage
}

type eventTokenMessage struct {
	modelName string
	tokens    claude.TokenUsage
}

func newAggregator() *aggregator {
	return &aggregator{
		summary: &claude.SessionTokenSummary{
			ModelBreakdown: make(map[string]*claude.ModelStats),
			ToolBreakdown:  make(map[string]*claude.ToolStats),
			BashBreakdown:  make(map[string]*claude.ToolStats),
		},
		messages: make(map[string]eventTokenMessage),
	}
}

func (a *aggregator) consume(ev model.Event) {
	switch ev.Subtype {
	case "MessageUpdated":
		var p map[string]any
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			return
		}
		if jsonutil.GetString(p, "message_role") != "assistant" {
			return
		}

		messageID := jsonutil.GetString(p, "message_id")
		if messageID == "" {
			messageID = fmt.Sprintf("event:%d", ev.ID)
		}

		modelName := jsonutil.GetString(p, "model_id")
		if modelName == "" {
			modelName = "unknown"
		}

		a.messages[messageID] = eventTokenMessage{
			modelName: modelName,
			tokens: claude.TokenUsage{
				InputTokens:         parseInt(jsonutil.GetString(p, "tokens_input")),
				OutputTokens:        parseInt(jsonutil.GetString(p, "tokens_output")),
				CacheReadTokens:     parseInt(jsonutil.GetString(p, "tokens_cache_read")),
				CacheCreationTokens: parseInt(jsonutil.GetString(p, "tokens_cache_write")),
			},
		}

	case "PreToolUse":
		if ev.ToolName == "" {
			return
		}
		// MCP tools are excluded from the per-tool breakdown to keep the
		// summary focused on first-class tools (Bash, Edit, Read, ...).
		if !strings.HasPrefix(ev.ToolName, "mcp__") {
			ts, ok := a.summary.ToolBreakdown[ev.ToolName]
			if !ok {
				ts = &claude.ToolStats{}
				a.summary.ToolBreakdown[ev.ToolName] = ts
			}
			ts.Calls++
		}

		if ev.ToolName == "Bash" || ev.ToolName == "BashTool" {
			var p map[string]any
			if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
				return
			}
			input := jsonutil.MapOrEmpty(p["tool_input"])
			if len(input) == 0 {
				input = jsonutil.MapOrEmpty(p["args"])
			}
			cmd := jsonutil.GetString(input, "command")
			for _, name := range claude.ExtractCommandNames(cmd) {
				bs, ok := a.summary.BashBreakdown[name]
				if !ok {
					bs = &claude.ToolStats{}
					a.summary.BashBreakdown[name] = bs
				}
				bs.Calls++
			}
		}
	}
}

func (a *aggregator) finalize() *claude.SessionTokenSummary {
	for _, msg := range a.messages {
		cost := claude.CalculateCost(msg.modelName, msg.tokens)

		a.summary.Tokens.Add(msg.tokens)
		a.summary.CostUSD += cost
		a.summary.APICalls++

		ms, ok := a.summary.ModelBreakdown[msg.modelName]
		if !ok {
			ms = &claude.ModelStats{}
			a.summary.ModelBreakdown[msg.modelName] = ms
		}
		ms.Calls++
		ms.Tokens.Add(msg.tokens)
		ms.CostUSD += cost
	}

	if a.summary.APICalls == 0 {
		return nil
	}
	return a.summary
}

func parseInt(s string) int64 {
	if s == "" || s == "0" {
		return 0
	}
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}
