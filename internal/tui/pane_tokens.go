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
	boxWidth := min(max(width-6, 40), 110)
	// Box overhead: border(2) + padding(2) + header(1) + blank(1) + centering margin(4) = 10
	viewHeight := min(max(height-10, 4), 50)
	totalLines := len(t.contentLines(boxWidth))
	return boxWidth, viewHeight, totalLines
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
	colW := max((bodyWidth-7)/2, 20)
	return renderTokenColumns(t.summary, colW)
}

func (t *tokensOverlay) viewFullScreen(width, height int) string {
	if !t.visible {
		return ""
	}

	boxWidth := min(max(width-6, 40), 110)
	// Box overhead: border(2) + padding(2) + header(1) + blank(1) + centering margin(4) = 10
	viewHeight := min(max(height-10, 4), 50)

	title := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("Token Usage")
	sessionHint := ""
	if t.session != nil {
		label := orDefault(t.session.Slug, shortID(t.session.ID))
		sessionHint = "  " + dimStyle.Render(label)
	}
	header := title + sessionHint

	lines := t.contentLines(boxWidth)
	totalLines := len(lines)

	// Clamp scroll.
	maxScroll := max(totalLines-viewHeight, 0)
	scroll := min(t.scroll, maxScroll)

	end := min(scroll+viewHeight, totalLines)
	visible := lines[scroll:end]

	// Pad to fixed height so box doesn't resize.
	allLines := append([]string{header, ""}, visible...)
	for len(allLines) < viewHeight+2 {
		allLines = append(allLines, "")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Padding(1, 2).
		Width(boxWidth).
		Render(strings.Join(allLines, "\n"))

	boxH := lipgloss.Height(box)
	boxW := lipgloss.Width(box)
	padTop := max((height-boxH)/2, 0)
	padLeft := max((width-boxW)/2, 0)

	// Build full-screen output line by line.
	var b strings.Builder
	boxLines := strings.Split(box, "\n")
	for row := 0; row < height; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		boxRow := row - padTop
		if boxRow >= 0 && boxRow < len(boxLines) {
			if padLeft > 0 {
				b.WriteString(strings.Repeat(" ", padLeft))
			}
			b.WriteString(boxLines[boxRow])
			// Fill remaining width.
			lineW := padLeft + boxW
			if lineW < width {
				b.WriteString(strings.Repeat(" ", width-lineW))
			}
		} else {
			b.WriteString(strings.Repeat(" ", width))
		}
	}

	return b.String()
}

// renderTokenColumns renders the token summary as a 2-column layout:
//
//	overview | model
//	tool usage | shell commands
func renderTokenColumns(s *claude.SessionTokenSummary, colW int) []string {
	overviewBlock := renderOverviewSection(s)
	modelBlock := renderModelSection(s)
	toolBlock := renderToolSection(s)
	bashBlock := renderBashSection(s)

	topLeft := renderColumnBlock("Overview", overviewBlock, colW)
	topRight := renderColumnBlock("Model", modelBlock, colW)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, "  ", topRight)

	botLeft := renderColumnBlock("Tool Usage", toolBlock, colW)
	botRight := renderColumnBlock("Shell Commands", bashBlock, colW)
	botRow := lipgloss.JoinHorizontal(lipgloss.Top, botLeft, "  ", botRight)

	full := topRow + "\n" + botRow
	return strings.Split(full, "\n")
}

func renderColumnBlock(title string, body []string, width int) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(title)
	content := header
	if len(body) > 0 {
		content += "\n" + strings.Join(body, "\n")
	} else {
		content += "\n" + dimStyle.Render("(none)")
	}
	return lipgloss.NewStyle().Width(width).Render(content)
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
	for _, m := range models {
		inTotal := m.stats.Tokens.InputTokens + m.stats.Tokens.CacheReadTokens + m.stats.Tokens.CacheCreationTokens
		lines = append(lines, fmt.Sprintf("  %s", m.name))
		lines = append(lines, fmt.Sprintf("    %s  calls:%d  total in:%s  out:%s",
			formatCost(m.stats.CostUSD),
			m.stats.Calls,
			formatTokenCount(inTotal),
			formatTokenCount(m.stats.Tokens.OutputTokens)))
	}
	return lines
}

func renderToolSection(s *claude.SessionTokenSummary) []string {
	if len(s.ToolBreakdown) == 0 {
		return nil
	}
	return renderBreakdownBar(s.ToolBreakdown)
}

func renderBashSection(s *claude.SessionTokenSummary) []string {
	if len(s.BashBreakdown) == 0 {
		return nil
	}
	return renderBreakdownBar(s.BashBreakdown)
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

func renderBreakdownBar(breakdown map[string]*claude.ToolStats) []string {
	var entries []breakdownEntry
	maxCalls := 0
	for name, stats := range breakdown {
		entries = append(entries, breakdownEntry{name, stats.Calls})
		if stats.Calls > maxCalls {
			maxCalls = stats.Calls
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].calls != entries[j].calls {
			return entries[i].calls > entries[j].calls
		}
		return entries[i].name < entries[j].name
	})

	if len(entries) > 15 {
		entries = entries[:15]
	}

	maxNameLen := 0
	for _, e := range entries {
		if len(e.name) > maxNameLen {
			maxNameLen = len(e.name)
		}
	}
	maxNameLen = min(maxNameLen, 16)

	const maxBarWidth = 15
	var lines []string
	for _, e := range entries {
		name := e.name
		if len(name) > 16 {
			name = name[:13] + "..."
		}
		padded := fmt.Sprintf("%-*s", maxNameLen, name)
		barLen := 0
		if maxCalls > 0 {
			barLen = e.calls * maxBarWidth / maxCalls
		}
		barLen = max(barLen, 1)
		bar := lipgloss.NewStyle().Foreground(colorCyan).Render(strings.Repeat("█", barLen))
		count := dimStyle.Render(fmt.Sprintf(" %d", e.calls))
		lines = append(lines, "  "+padded+" "+bar+count)
	}
	return lines
}
