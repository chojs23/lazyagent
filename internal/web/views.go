package web

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/diff"
	"github.com/chojs23/lazyagent/internal/eventview"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
)

// usageViewModel is a JSON-friendly mirror of claude.SessionTokenSummary.
// The internal model uses pointer-valued maps for breakdowns; we flatten
// those into ranked slices so the frontend gets a stable, sorted shape it
// can render directly without re-sorting.
type tokenUsageView struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
}

type modelStatsView struct {
	Model   string         `json:"model"`
	Calls   int            `json:"calls"`
	Tokens  tokenUsageView `json:"tokens"`
	CostUSD float64        `json:"cost_usd"`
}

type toolCallView struct {
	Name  string `json:"name"`
	Calls int    `json:"calls"`
}

type usageViewModel struct {
	APICalls       int              `json:"api_calls"`
	CostUSD        float64          `json:"cost_usd"`
	Tokens         tokenUsageView   `json:"tokens"`
	ModelBreakdown []modelStatsView `json:"model_breakdown,omitempty"`
	ToolBreakdown  []toolCallView   `json:"tool_breakdown,omitempty"`
	BashBreakdown  []toolCallView   `json:"bash_breakdown,omitempty"`
}

func usageView(s *claude.SessionTokenSummary) usageViewModel {
	out := usageViewModel{
		APICalls: s.APICalls,
		CostUSD:  s.CostUSD,
		Tokens: tokenUsageView{
			InputTokens:         s.Tokens.InputTokens,
			OutputTokens:        s.Tokens.OutputTokens,
			CacheReadTokens:     s.Tokens.CacheReadTokens,
			CacheCreationTokens: s.Tokens.CacheCreationTokens,
		},
	}

	for name, ms := range s.ModelBreakdown {
		out.ModelBreakdown = append(out.ModelBreakdown, modelStatsView{
			Model: name,
			Calls: ms.Calls,
			Tokens: tokenUsageView{
				InputTokens:         ms.Tokens.InputTokens,
				OutputTokens:        ms.Tokens.OutputTokens,
				CacheReadTokens:     ms.Tokens.CacheReadTokens,
				CacheCreationTokens: ms.Tokens.CacheCreationTokens,
			},
			CostUSD: ms.CostUSD,
		})
	}
	// Highest token volume first; cost is correlated but not the same so we
	// sort by total tokens to mirror the TUI's "Top Model" ranking.
	sort.Slice(out.ModelBreakdown, func(i, j int) bool {
		return totalUsage(out.ModelBreakdown[i].Tokens) > totalUsage(out.ModelBreakdown[j].Tokens)
	})

	for name, ts := range s.ToolBreakdown {
		out.ToolBreakdown = append(out.ToolBreakdown, toolCallView{Name: name, Calls: ts.Calls})
	}
	sort.Slice(out.ToolBreakdown, func(i, j int) bool { return out.ToolBreakdown[i].Calls > out.ToolBreakdown[j].Calls })

	for name, ts := range s.BashBreakdown {
		out.BashBreakdown = append(out.BashBreakdown, toolCallView{Name: name, Calls: ts.Calls})
	}
	sort.Slice(out.BashBreakdown, func(i, j int) bool { return out.BashBreakdown[i].Calls > out.BashBreakdown[j].Calls })

	return out
}

func totalUsage(t tokenUsageView) int64 {
	return t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheCreationTokens
}

// View structs translate internal model types into JSON shapes that the
// frontend consumes. Keeping them separate from model.* avoids leaking
// database column quirks (e.g. zero-valued StoppedAt) into the API surface.

type projectView struct {
	ID             int64  `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Directory      string `json:"directory,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	SessionCount   int64  `json:"session_count"`
	UpdatedAt      int64  `json:"updated_at"`
}

type sessionViewModel struct {
	ID              string `json:"id"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ProjectID       int64  `json:"project_id"`
	ProjectSlug     string `json:"project_slug,omitempty"`
	ProjectName     string `json:"project_name,omitempty"`
	Slug            string `json:"slug,omitempty"`
	Status          string `json:"status"`
	Runtime         string `json:"runtime"`
	StartedAt       int64  `json:"started_at"`
	StoppedAt       int64  `json:"stopped_at,omitempty"`
	LastActivity    int64  `json:"last_activity,omitempty"`
	EventCount      int64  `json:"event_count"`
	AgentCount      int64  `json:"agent_count"`
	TranscriptPath  string `json:"transcript_path,omitempty"`
}

type agentView struct {
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	ParentAgentID string `json:"parent_agent_id,omitempty"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	AgentType     string `json:"agent_type,omitempty"`
	AgentClass    string `json:"agent_class,omitempty"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
}

type eventView struct {
	ID          int64  `json:"id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	Type        string `json:"type"`
	Subtype     string `json:"subtype,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolUseID   string `json:"tool_use_id,omitempty"`
	Timestamp   int64  `json:"timestamp"`
	Brief       string `json:"brief,omitempty"`
	Highlighted bool   `json:"highlighted,omitempty"`
}

type eventDetailViewModel struct {
	eventView
	// Payload is the parsed JSON payload as a generic value. If the stored
	// payload is not valid JSON the raw string is returned under "raw" so
	// the frontend can still render something useful.
	Payload   json.RawMessage `json:"payload,omitempty"`
	Raw       string          `json:"raw,omitempty"`
	DiffLines []diffLineView  `json:"diff_lines,omitempty"`
	DiffStats *diffStatsView  `json:"diff_stats,omitempty"`
	DiffPath  string          `json:"diff_path,omitempty"`
}

// diffLineView mirrors a diff.Line but uses string ops so the frontend
// does not need to know the int enum values.
type diffLineView struct {
	Op   string `json:"op"` // "equal" | "delete" | "insert" | "gap"
	Text string `json:"text"`
}

type diffStatsView struct {
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

func projectsView(rows []model.Project) []projectView {
	out := make([]projectView, 0, len(rows))
	for _, p := range rows {
		out = append(out, projectView{
			ID:             p.ID,
			Slug:           p.Slug,
			Name:           p.Name,
			Directory:      p.Directory,
			TranscriptPath: p.TranscriptPath,
			SessionCount:   p.SessionCount,
			UpdatedAt:      p.UpdatedAt,
		})
	}
	return out
}

func sessionsView(rows []model.Session) []sessionViewModel {
	out := make([]sessionViewModel, 0, len(rows))
	for _, s := range rows {
		out = append(out, sessionView(s))
	}
	return out
}

func sessionView(s model.Session) sessionViewModel {
	return sessionViewModel{
		ID:              s.ID,
		ParentSessionID: s.ParentSessionID,
		ProjectID:       s.ProjectID,
		ProjectSlug:     s.ProjectSlug,
		ProjectName:     s.ProjectName,
		Slug:            s.Slug,
		Status:          s.Status,
		Runtime:         s.Runtime,
		StartedAt:       s.StartedAt,
		StoppedAt:       s.StoppedAt,
		LastActivity:    s.LastActivity,
		EventCount:      s.EventCount,
		AgentCount:      s.AgentCount,
		TranscriptPath:  s.TranscriptPath,
	}
}

func agentsView(rows []model.Agent) []agentView {
	out := make([]agentView, 0, len(rows))
	for _, a := range rows {
		out = append(out, agentView{
			ID:            a.ID,
			SessionID:     a.SessionID,
			ParentAgentID: a.ParentAgentID,
			Name:          a.Name,
			Description:   a.Description,
			AgentType:     a.AgentType,
			AgentClass:    a.AgentClass,
			Status:        a.Status,
			CreatedAt:     a.CreatedAt,
		})
	}
	return out
}

func eventsView(rows []model.Event) []eventView {
	out := make([]eventView, 0, len(rows))
	for _, e := range rows {
		out = append(out, makeEventView(e))
	}
	return out
}

func makeEventView(e model.Event) eventView {
	return eventView{
		ID:          e.ID,
		AgentID:     e.AgentID,
		SessionID:   e.SessionID,
		Type:        e.Type,
		Subtype:     e.Subtype,
		ToolName:    e.ToolName,
		ToolUseID:   e.ToolUseID,
		Timestamp:   e.Timestamp,
		Brief:       eventview.Brief(e),
		Highlighted: eventview.IsHighlighted(e),
	}
}

// eventDetailView returns a full event including its payload. When the stored
// payload parses as JSON it is returned as a structured "payload" field so the
// frontend can render it inline; otherwise it falls back to the raw string.
//
// For Edit / apply_patch tool events we additionally precompute a structured
// diff (Myers + 3-line context) on the server so the frontend can render the
// hunks the same way the TUI does without reimplementing diff in JS.
func eventDetailView(e model.Event) eventDetailViewModel {
	out := eventDetailViewModel{eventView: makeEventView(e)}
	if e.Payload == "" {
		return out
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(e.Payload), &probe); err == nil {
		out.Payload = json.RawMessage(e.Payload)
		populateDiff(&out, e, probe)
	} else {
		out.Raw = e.Payload
	}
	return out
}

func populateDiff(out *eventDetailViewModel, e model.Event, payload map[string]any) {
	if e.ToolName != "Edit" && e.ToolName != "apply_patch" {
		return
	}
	input := jsonutil.MapOrEmpty(payload["tool_input"])
	if len(input) == 0 {
		input = jsonutil.MapOrEmpty(payload["args"])
	}

	if e.ToolName == "Edit" {
		oldStr := firstNonEmpty(jsonutil.GetString(input, "old_string"), jsonutil.GetString(input, "oldString"))
		newStr := firstNonEmpty(jsonutil.GetString(input, "new_string"), jsonutil.GetString(input, "newString"))
		if oldStr == "" && newStr == "" {
			// PostToolUse Edits sometimes only carry a unified diff in the
			// metadata block; fall back to that so the row still has a
			// useful structured view.
			if meta := jsonutil.MapOrEmpty(payload["metadata"]); len(meta) > 0 {
				if patch := jsonutil.GetString(meta, "diff"); patch != "" {
					out.DiffLines, out.DiffStats = parseUnifiedPatch(patch)
				}
			}
			out.DiffPath = firstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"))
			return
		}
		out.DiffPath = firstNonEmpty(jsonutil.GetString(input, "file_path"), jsonutil.GetString(input, "filePath"))
		out.DiffLines, out.DiffStats = computeStructuredDiff(oldStr, newStr)
		return
	}

	// apply_patch: input may carry the patch itself as a single string, or
	// the diff may be in payload.metadata.diff.
	patch := firstNonEmpty(
		jsonutil.GetString(input, "input"),
		jsonutil.GetString(input, "patch"),
		jsonutil.GetString(input, "patchText"),
	)
	if patch == "" {
		if meta := jsonutil.MapOrEmpty(payload["metadata"]); len(meta) > 0 {
			patch = jsonutil.GetString(meta, "diff")
		}
	}
	if patch != "" {
		out.DiffLines, out.DiffStats = parseUnifiedPatch(patch)
	}
}

// computeStructuredDiff returns the same 3-line-context Myers diff that the
// TUI's pane_detail uses, as a flat list of typed lines plus add/delete
// counts. Hunk boundaries are emitted as Op="gap" lines so the renderer
// can visually mark omitted regions.
func computeStructuredDiff(oldStr, newStr string) ([]diffLineView, *diffStatsView) {
	oldLines := splitDiffLines(oldStr)
	newLines := splitDiffLines(newStr)
	script := diff.Compute(oldLines, newLines)
	stats := diff.Count(script)
	context := diff.WithContext(script, 3)

	out := make([]diffLineView, 0, len(context))
	for _, dl := range context {
		switch dl.Op {
		case diff.OpDelete:
			out = append(out, diffLineView{Op: "delete", Text: dl.Text})
		case diff.OpInsert:
			out = append(out, diffLineView{Op: "insert", Text: dl.Text})
		case diff.OpEqual:
			if dl.Text == "~~~" {
				out = append(out, diffLineView{Op: "gap"})
			} else {
				out = append(out, diffLineView{Op: "equal", Text: dl.Text})
			}
		}
	}
	return out, &diffStatsView{Additions: stats.Additions, Deletions: stats.Deletions}
}

// parseUnifiedPatch turns a unified-diff / apply_patch text into the same
// typed line shape as computeStructuredDiff so the frontend can render
// either source identically. Header lines (---, +++, ***) and hunk
// markers (@@) are dropped.
func parseUnifiedPatch(patch string) ([]diffLineView, *diffStatsView) {
	if patch == "" {
		return nil, nil
	}
	// Patches usually end with a newline. Without trimming, strings.Split
	// emits a phantom empty equal-line at the bottom of every diff block.
	patch = strings.TrimRight(patch, "\n")
	var out []diffLineView
	stats := diffStatsView{}
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "***"):
			// patch header lines, skip
		case strings.HasPrefix(line, "@@"):
			out = append(out, diffLineView{Op: "gap", Text: strings.TrimSpace(line)})
		case strings.HasPrefix(line, "+"):
			out = append(out, diffLineView{Op: "insert", Text: strings.TrimPrefix(line, "+")})
			stats.Additions++
		case strings.HasPrefix(line, "-"):
			out = append(out, diffLineView{Op: "delete", Text: strings.TrimPrefix(line, "-")})
			stats.Deletions++
		default:
			out = append(out, diffLineView{Op: "equal", Text: strings.TrimPrefix(line, " ")})
		}
	}
	return out, &stats
}

func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
