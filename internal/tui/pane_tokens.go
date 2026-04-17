package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chojs23/lazyagent/internal/claude"
	"github.com/chojs23/lazyagent/internal/codex"
	"github.com/chojs23/lazyagent/internal/jsonutil"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
)

type tokensOverlay struct {
	visible bool
	scroll  int
	summary *claude.SessionTokenSummary
	session *model.Session
	err     string
}

var tokenEventPageSize = 2000

func (t *tokensOverlay) toggle(session *model.Session, st *store.Store) {
	if t.visible {
		t.visible = false
		return
	}
	t.visible = true
	t.scroll = 0
	t.session = session
	t.err = ""
	t.summary = nil
	t.load(session, st)
}

func (t *tokensOverlay) close() {
	t.visible = false
}

func (t *tokensOverlay) load(session *model.Session, st *store.Store) {
	if session == nil {
		t.err = "No session selected"
		return
	}

	switch session.Runtime {
	case "claude":
		t.loadClaude(session)
	case "codex":
		t.loadCodex(session)
	default:
		t.loadFromEvents(session, st)
	}
}

func (t *tokensOverlay) loadClaude(session *model.Session) {
	tp := session.TranscriptPath
	if tp == "" {
		t.err = "No transcript path"
		return
	}
	summary, err := claude.ReadTranscriptTokens(tp)
	if err != nil {
		t.err = "Failed to read transcript: " + err.Error()
		return
	}
	t.summary = summary
}

func (t *tokensOverlay) loadCodex(session *model.Session) {
	tp := session.TranscriptPath
	if tp == "" {
		t.err = "No transcript path"
		return
	}
	summary, err := codex.ReadTranscriptTokens(tp)
	if err != nil {
		t.err = "Failed to read transcript: " + err.Error()
		return
	}
	if summary == nil {
		t.err = "No token data available"
		return
	}
	t.summary = summary
}

func (t *tokensOverlay) loadFromEvents(session *model.Session, st *store.Store) {
	if st == nil {
		t.err = "No store"
		return
	}

	ctx := context.Background()
	q := st.Read()
	summary, err := readEventTokenSummary(ctx, q, session.ID)
	if err != nil {
		t.err = "Failed to load events: " + err.Error()
		return
	}
	if summary == nil || summary.APICalls == 0 {
		t.err = "No token data available"
		return
	}
	t.summary = summary
}

type eventTokenMessage struct {
	modelName string
	tokens    claude.TokenUsage
}

type eventTokenAggregator struct {
	summary  *claude.SessionTokenSummary
	messages map[string]eventTokenMessage
}

func newEventTokenAggregator() *eventTokenAggregator {
	return &eventTokenAggregator{
		summary: &claude.SessionTokenSummary{
			ModelBreakdown: make(map[string]*claude.ModelStats),
			ToolBreakdown:  make(map[string]*claude.ToolStats),
			BashBreakdown:  make(map[string]*claude.ToolStats),
		},
		messages: make(map[string]eventTokenMessage),
	}
}

func readEventTokenSummary(ctx context.Context, q *store.Queries, sessionID string) (*claude.SessionTokenSummary, error) {
	agg := newEventTokenAggregator()
	offset := 0

	for {
		events, err := q.ListEventsForSessionTree(ctx, sessionID, model.EventFilter{
			Limit:  tokenEventPageSize,
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
		if len(events) < tokenEventPageSize {
			break
		}
	}

	return agg.finalize(), nil
}

func (a *eventTokenAggregator) consume(ev model.Event) {
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
				InputTokens:         parseTokenInt(jsonutil.GetString(p, "tokens_input")),
				OutputTokens:        parseTokenInt(jsonutil.GetString(p, "tokens_output")),
				CacheReadTokens:     parseTokenInt(jsonutil.GetString(p, "tokens_cache_read")),
				CacheCreationTokens: parseTokenInt(jsonutil.GetString(p, "tokens_cache_write")),
			},
		}

	case "PreToolUse":
		if ev.ToolName == "" {
			return
		}
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

func (a *eventTokenAggregator) finalize() *claude.SessionTokenSummary {
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

func parseTokenInt(s string) int64 {
	if s == "" || s == "0" {
		return 0
	}
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

func (t *tokensOverlay) calcView(width, height int) (int, int, int) {
	boxWidth, viewHeight := tokenOverlayLayout(width, height)
	totalLines := len(t.contentLines(boxWidth))
	return boxWidth, viewHeight, totalLines
}

func tokenOverlayLayout(width, height int) (int, int) {
	boxWidth := min(max(width-4, 24), 118)
	viewHeight := min(max(height-11, 1), 34)
	return boxWidth, viewHeight
}

func (t *tokensOverlay) scrollUp(n int) {
	t.scroll = max(t.scroll-n, 0)
}

func (t *tokensOverlay) scrollDown(n, width, height int) {
	_, viewHeight, totalLines := t.calcView(width, height)
	maxScroll := max(totalLines-viewHeight, 0)
	t.scroll = min(t.scroll+n, maxScroll)
}

func (t *tokensOverlay) halfPageUp(width, height int) {
	_, viewHeight, _ := t.calcView(width, height)
	t.scroll = max(t.scroll-viewHeight/2, 0)
}

func (t *tokensOverlay) halfPageDown(width, height int) {
	_, viewHeight, totalLines := t.calcView(width, height)
	maxScroll := max(totalLines-viewHeight, 0)
	t.scroll = min(t.scroll+viewHeight/2, maxScroll)
}

// contentLines builds the rendered content lines for the current summary.
// This is called from view() which runs on a value-receiver copy,
// so we cannot cache the result. The summary data is small enough
// that rebuilding each frame is fine.
func (t *tokensOverlay) contentLines(bodyWidth int) []string {
	if t.err != "" {
		return []string{dimStyle.Render(t.err)}
	}
	if t.summary == nil {
		return []string{dimStyle.Render("(no data)")}
	}
	return renderTokenColumns(t.session, t.summary, max(bodyWidth-4, 20))
}

func (t *tokensOverlay) viewFullScreen(width, height int) string {
	if !t.visible {
		return ""
	}

	boxWidth, viewHeight := tokenOverlayLayout(width, height)

	title := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("Usage Audit")
	sessionHint := dimStyle.Render("No session selected")
	if t.session != nil {
		label := orDefault(t.session.Slug, shortID(t.session.ID))
		sessionHint = dimStyle.Render(runtimeLabel(t.session.Runtime) + "  " + label)
	}
	hints := dimStyle.Render("j/k scroll  ctrl+u/d jump  esc close")
	headerLines := []string{
		title + "  " + sessionHint,
		hints,
		dimStyle.Render(strings.Repeat("─", max(boxWidth-6, 8))),
		"",
	}

	lines := t.contentLines(boxWidth)
	totalLines := len(lines)

	maxScroll := max(totalLines-viewHeight, 0)
	scroll := min(t.scroll, maxScroll)

	end := min(scroll+viewHeight, totalLines)
	visible := lines[scroll:end]

	allLines := append(append([]string{}, headerLines...), visible...)
	for len(allLines) < len(headerLines)+viewHeight {
		allLines = append(allLines, "")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Padding(2, 2, 1, 2).
		Width(boxWidth).
		Render(strings.Join(allLines, "\n"))

	return box
}

func renderTokenColumns(session *model.Session, s *claude.SessionTokenSummary, bodyWidth int) []string {
	if bodyWidth < 86 {
		stackWidth := max(bodyWidth, 20)
		stacked := strings.Join([]string{
			renderSectionBlock("Session", renderSessionSection(session), stackWidth),
			renderSectionBlock("Overview", renderOverviewSection(s), stackWidth),
			renderSectionBlock("Signals", renderSignalsSection(s), stackWidth),
			renderSectionBlock("Model Ledger", renderModelSection(s), stackWidth),
			renderSectionBlock("Execution Mix", renderExecutionSection(s), stackWidth),
		}, "\n\n")
		return strings.Split(stacked, "\n")
	}

	leftWidth := min(max(bodyWidth/3, 24), 30)
	mainWidth := max(bodyWidth-leftWidth-3, 24)
	left := strings.Join([]string{
		renderSectionBlock("Session", renderSessionSection(session), leftWidth),
		renderSectionBlock("Overview", renderOverviewSection(s), leftWidth),
		renderSectionBlock("Signals", renderSignalsSection(s), leftWidth),
	}, "\n\n")
	right := strings.Join([]string{
		renderSectionBlock("Model Ledger", renderModelSection(s), mainWidth),
		renderSectionBlock("Execution Mix", renderExecutionSection(s), mainWidth),
	}, "\n\n")

	full := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(left),
		"   ",
		lipgloss.NewStyle().Width(mainWidth).Render(right),
	)
	return strings.Split(full, "\n")
}

func renderSectionBlock(title string, body []string, width int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(title)
	divider := dimStyle.Render(strings.Repeat("─", max(width-2, 10)))
	content := header + "\n" + divider
	if len(body) > 0 {
		content += "\n" + strings.Join(body, "\n")
	} else {
		content += "\n" + dimStyle.Render("(none)")
	}
	return lipgloss.NewStyle().Width(width).Render(content)
}

func renderSessionSection(session *model.Session) []string {
	if session == nil {
		return []string{dimStyle.Render("No session selected")}
	}

	project := orDefault(session.ProjectName, session.ProjectSlug)
	if project == "" {
		project = "-"
	}

	return []string{
		tokenField("Runtime", runtimeLabel(session.Runtime)),
		tokenField("Session", orDefault(session.Slug, shortID(session.ID))),
		tokenField("Project", project),
	}
}

func renderOverviewSection(s *claude.SessionTokenSummary) []string {
	totalInput := s.Tokens.InputTokens + s.Tokens.CacheReadTokens + s.Tokens.CacheCreationTokens
	lines := []string{
		tokenField("Cost", formatCost(s.CostUSD)),
		tokenField("Model Calls", fmt.Sprintf("%d", s.APICalls)),
		tokenField("Direct Input", formatTokenCount(s.Tokens.InputTokens)),
		tokenField("Total Input", formatTokenCount(totalInput)),
		tokenField("Output", formatTokenCount(s.Tokens.OutputTokens)),
		tokenField("Cache Read", formatTokenCount(s.Tokens.CacheReadTokens)),
		tokenField("Cache Write", formatTokenCount(s.Tokens.CacheCreationTokens)),
	}
	if totalInput > 0 {
		hitRate := float64(s.Tokens.CacheReadTokens) / float64(totalInput) * 100
		lines = append(lines, tokenField("Cache Hit", fmt.Sprintf("%.1f%%", hitRate)))
	}
	return lines
}

func renderModelSection(s *claude.SessionTokenSummary) []string {
	if len(s.ModelBreakdown) == 0 {
		return nil
	}
	type modelEntry struct {
		name  string
		stats *claude.ModelStats
	}
	var models []modelEntry
	for name, stats := range s.ModelBreakdown {
		models = append(models, modelEntry{name, stats})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].stats.Tokens.Total() != models[j].stats.Tokens.Total() {
			return models[i].stats.Tokens.Total() > models[j].stats.Tokens.Total()
		}
		return models[i].name < models[j].name
	})

	var lines []string
	totalTokens := max(s.Tokens.Total(), 1)
	limit := min(len(models), 6)
	for idx, m := range models[:limit] {
		inTotal := m.stats.Tokens.InputTokens + m.stats.Tokens.CacheReadTokens + m.stats.Tokens.CacheCreationTokens
		share := float64(m.stats.Tokens.Total()) / float64(totalTokens) * 100
		lines = append(lines, fmt.Sprintf("%d. %s", idx+1, m.name))
		lines = append(lines, fmt.Sprintf("   %s  share:%4.1f%%  calls:%d  total in:%s  out:%s",
			formatCost(m.stats.CostUSD),
			share,
			m.stats.Calls,
			formatTokenCount(inTotal),
			formatTokenCount(m.stats.Tokens.OutputTokens)))
	}
	if len(models) > limit {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("… %d more models", len(models)-limit)))
	}
	return lines
}

func renderSignalsSection(s *claude.SessionTokenSummary) []string {
	totalInput := s.Tokens.InputTokens + s.Tokens.CacheReadTokens + s.Tokens.CacheCreationTokens
	lines := []string{}
	if totalInput > 0 {
		cacheShare := float64(s.Tokens.CacheReadTokens) / float64(totalInput) * 100
		lines = append(lines, tokenField("Cache Share", fmt.Sprintf("%.1f%%", cacheShare)))
	}
	if totalInput > 0 {
		outputRatio := float64(s.Tokens.OutputTokens) / float64(totalInput) * 100
		lines = append(lines, tokenField("Output Ratio", fmt.Sprintf("%.1f%%", outputRatio)))
	}
	if topModel, share := topModelSignal(s); topModel != "" {
		lines = append(lines, tokenField("Top Model", fmt.Sprintf("%s (%0.1f%%)", topModel, share)))
	}
	if topTool := topBreakdownName(s.ToolBreakdown); topTool != "" {
		lines = append(lines, tokenField("Top Tool", topTool))
	}
	if topCmd := topBreakdownName(s.BashBreakdown); topCmd != "" {
		lines = append(lines, tokenField("Top Command", topCmd))
	}
	return lines
}

func renderExecutionSection(s *claude.SessionTokenSummary) []string {
	toolLines := renderRankedBreakdown("Tools", s.ToolBreakdown, "tool", 5)
	cmdLines := renderRankedBreakdown("Commands", s.BashBreakdown, "$", 5)
	if len(toolLines) == 0 && len(cmdLines) == 0 {
		return nil
	}
	if len(toolLines) == 0 {
		return cmdLines
	}
	if len(cmdLines) == 0 {
		return toolLines
	}
	return append(append(toolLines, ""), cmdLines...)
}

func tokenField(label, value string) string {
	return "  " + subtitleStyle.Render(label+":") + " " + value
}

func formatCost(usd float64) string {
	if usd == 0 {
		return "$0.00"
	}
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

type breakdownEntry struct {
	name  string
	calls int
}

func renderRankedBreakdown(title string, breakdown map[string]*claude.ToolStats, marker string, limit int) []string {
	entries, totalCalls := breakdownEntries(breakdown)
	if len(entries) == 0 {
		return nil
	}
	entries = entries[:min(len(entries), limit)]

	lines := []string{subtitleStyle.Render(title)}
	for idx, e := range entries {
		share := 0.0
		if totalCalls > 0 {
			share = float64(e.calls) / float64(totalCalls) * 100
		}
		lines = append(lines, fmt.Sprintf("  %d. %-2s %-14s %4.1f%%  %d calls",
			idx+1,
			marker,
			truncate(e.name, 14),
			share,
			e.calls,
		))
	}
	return lines
}

func breakdownEntries(breakdown map[string]*claude.ToolStats) ([]breakdownEntry, int) {
	var entries []breakdownEntry
	totalCalls := 0
	for name, stats := range breakdown {
		entries = append(entries, breakdownEntry{name, stats.Calls})
		totalCalls += stats.Calls
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].calls != entries[j].calls {
			return entries[i].calls > entries[j].calls
		}
		return entries[i].name < entries[j].name
	})
	return entries, totalCalls
}

func topModelSignal(s *claude.SessionTokenSummary) (string, float64) {
	if len(s.ModelBreakdown) == 0 {
		return "", 0
	}
	entries := make([]breakdownEntry, 0, len(s.ModelBreakdown))
	total := int64(0)
	for name, stats := range s.ModelBreakdown {
		entries = append(entries, breakdownEntry{name: name, calls: int(stats.Tokens.Total())})
		total += stats.Tokens.Total()
	}
	if total == 0 {
		return "", 0
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].calls != entries[j].calls {
			return entries[i].calls > entries[j].calls
		}
		return entries[i].name < entries[j].name
	})
	share := float64(entries[0].calls) / float64(total) * 100
	return entries[0].name, share
}

func topBreakdownName(breakdown map[string]*claude.ToolStats) string {
	entries, _ := breakdownEntries(breakdown)
	if len(entries) == 0 {
		return ""
	}
	return entries[0].name
}
