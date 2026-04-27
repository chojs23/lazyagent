// Frontend for lazyagent web UI.
//
// State model: a single `state` object holds the currently selected project,
// session, and event ID. Render functions are pure with respect to that
// state and any fetched data, so the auto-refresh tick can call them
// repeatedly without leaking UI state.

const state = {
  projectId: null,
  sessionId: null,
  eventId: null,
  filter: { type: "", search: "", agent: "" },
  // Bumped on every operation that invalidates in-flight session-scoped
  // fetches (session change, filter change, full reload). Async loaders
  // capture this at entry and bail when it changes mid-flight, so a stale
  // response never overwrites a newer view.
  gen: 0,
  // expanded holds the set of project ids whose sessions are currently shown
  // in the sidebar tree. Persisting this on the client (rather than
  // re-fetching on every refresh tick) means the tree never collapses on
  // its own when the auto-refresh fires.
  expanded: new Set(),
  detailEvent: null,
  detailShowJSON: false,
  cache: {
    projects: [],
    sessionsByProject: new Map(),
    agents: [],
    events: [],
  },
  // TUI-style paged event window:
  //   list   = the events currently held in memory (chronological)
  //   offset = absolute offset of list[0] in the filtered server result
  //   total  = total number of events matching the current filter
  //   loading flags prevent overlapping prepend/append fetches
  pager: {
    pageSize: 500,
    offset: 0,
    total: 0,
    autoFollow: true,
    loadingOlder: false,
    loadingNewer: false,
  },
};

const REFRESH_MS = 2000;

const els = {
  projectTree: document.getElementById("project-tree"),
  agentList: document.getElementById("agent-list"),
  agentsCount: document.getElementById("agents-count"),
  eventList: document.getElementById("event-list"),
  eventsCount: document.getElementById("events-count"),
  sessionMeta: document.getElementById("session-meta"),
  eventDetail: document.getElementById("event-detail"),
  filterType: document.getElementById("filter-type"),
  filterSearch: document.getElementById("filter-search"),
  autoRefresh: document.getElementById("auto-refresh"),
  refreshBtn: document.getElementById("refresh-btn"),
  usageBtn: document.getElementById("usage-btn"),
  usageModal: document.getElementById("usage-modal"),
  usageBody: document.getElementById("usage-body"),
};

async function fetchJSON(url) {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

function fmtTime(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  const pad = (n) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function fmtDate(ms) {
  if (!ms) return "";
  const d = new Date(ms);
  return d.toLocaleString();
}

function runtimeTag(runtime) {
  const map = { claude: "C", codex: "X", opencode: "O" };
  return map[runtime] || runtime?.[0]?.toUpperCase() || "?";
}

async function loadProjects() {
  const data = await fetchJSON("/api/projects");
  state.cache.projects = data.projects || [];
  // Refresh sessions for any expanded project so counts and statuses stay
  // current. Each fetch is independent so we run them in parallel.
  await Promise.all(
    Array.from(state.expanded).map((pid) => loadSessionsForProject(pid))
  );
  renderTree();
}

async function loadSessionsForProject(projectId) {
  const data = await fetchJSON(`/api/projects/${projectId}/sessions`);
  state.cache.sessionsByProject.set(projectId, data.sessions || []);
}

function renderTree() {
  els.projectTree.innerHTML = "";
  state.cache.projects.forEach((p) => {
    els.projectTree.appendChild(renderProjectNode(p));
  });
}

function renderProjectNode(p) {
  const li = document.createElement("li");
  li.dataset.id = p.id;

  const isOpen = state.expanded.has(p.id);
  const isSelected = p.id === state.projectId && !state.sessionId;

  const row = document.createElement("div");
  row.className = "row project-row" + (isSelected ? " selected" : "");
  const caretClass = p.session_count === 0 ? "caret empty" : "caret" + (isOpen ? " open" : "");
  row.innerHTML = `
    <span class="${caretClass}">▶</span>
    <span class="title">${escapeHTML(p.name)}</span>
    <span class="count">${p.session_count}</span>
  `;
  row.addEventListener("click", () => toggleProject(p.id));
  li.appendChild(row);

  const childUL = document.createElement("ul");
  childUL.className = "session-children" + (isOpen ? "" : " collapsed");
  if (isOpen) {
    const sessions = state.cache.sessionsByProject.get(p.id) || [];
    if (sessions.length === 0) {
      const empty = document.createElement("li");
      empty.className = "empty-msg";
      empty.textContent = "no sessions";
      childUL.appendChild(empty);
    } else {
      sessions.forEach((s) => childUL.appendChild(renderSessionNode(s)));
    }
  }
  li.appendChild(childUL);
  return li;
}

function renderSessionNode(s) {
  const li = document.createElement("li");
  const isSelected = s.id === state.sessionId;
  const isActive = s.status === "active";
  const row = document.createElement("div");
  row.className = "row session-row" + (isSelected ? " selected" : "");
  row.innerHTML = `
    <span class="status-dot ${isActive ? "active" : ""}"></span>
    <span class="runtime-tag">${runtimeTag(s.runtime)}</span>
    <span class="title">${escapeHTML(s.slug || s.id.slice(0, 8))}</span>
    <span class="meta">${s.event_count}</span>
  `;
  row.addEventListener("click", (e) => {
    e.stopPropagation();
    selectSession(s.id);
  });
  li.appendChild(row);
  return li;
}

async function toggleProject(id) {
  // Collapsing only requires UI state; expanding may need to fetch sessions
  // we have not loaded yet. We always re-render afterwards so the caret and
  // selection state stay in sync.
  state.projectId = id;
  if (state.expanded.has(id)) {
    state.expanded.delete(id);
  } else {
    state.expanded.add(id);
    if (!state.cache.sessionsByProject.has(id)) {
      try {
        await loadSessionsForProject(id);
      } catch (err) {
        console.error("load sessions failed", err);
      }
    }
  }
  renderTree();
}

async function selectSession(id) {
  state.sessionId = id;
  state.eventId = null;
  state.gen++;
  // Clear the agent filter — agent IDs from a previous session don't apply
  // here. Type/search filters carry over since they're session-agnostic.
  state.filter.agent = "";
  renderTree();
  await Promise.all([loadAgents(), loadEvents()]);
  renderSessionMeta();
  if (els.usageBtn) els.usageBtn.disabled = !id;
}

function findSession(id) {
  for (const sessions of state.cache.sessionsByProject.values()) {
    const found = sessions.find((s) => s.id === id);
    if (found) return found;
  }
  return null;
}

function renderSessionMeta() {
  const session = findSession(state.sessionId);
  if (!session) {
    els.sessionMeta.textContent = "no session selected";
    els.sessionMeta.classList.add("muted");
    return;
  }
  els.sessionMeta.classList.remove("muted");
  // Title mirrors the left tree — slug if present, otherwise the short id.
  const title = session.slug || session.id.slice(0, 8);
  els.sessionMeta.innerHTML = `
    <div class="meta-line meta-title-line">
      <span class="runtime-tag">${runtimeTag(session.runtime)}</span>
      <span class="meta-title">${escapeHTML(title)}</span>
    </div>
    <div class="meta-line">
      <span class="meta-id">${escapeHTML(session.id)}</span>
      <span class="meta-sep">—</span>
      <span class="meta-started">started ${fmtDate(session.started_at)}</span>
    </div>
  `;
}

async function loadAgents() {
  if (!state.sessionId) return;
  const myGen = state.gen;
  const data = await fetchJSON(`/api/sessions/${state.sessionId}/agents`);
  if (myGen !== state.gen) return;
  state.cache.agents = data.agents || [];
  // Build a lookup so the events list can resolve agent_id → display name
  // and a stable color slot without scanning the array per row.
  state.cache.agentsByID = new Map();
  state.cache.agents.forEach((a, idx) => {
    state.cache.agentsByID.set(a.id, { ...a, _index: idx });
  });
  renderAgents();
  // Re-render events so newly loaded agent names propagate into the rows.
  if (state.cache.events.length > 0) renderEvents();
}

function renderAgents() {
  els.agentList.innerHTML = "";
  if (els.agentsCount) {
    els.agentsCount.textContent = state.cache.agents.length > 0 ? `(${state.cache.agents.length})` : "";
  }
  if (state.cache.agents.length === 0) {
    const li = document.createElement("li");
    li.className = "muted";
    li.textContent = "no agents";
    els.agentList.appendChild(li);
    return;
  }
  state.cache.agents.forEach((a, idx) => {
    const li = document.createElement("li");
    const isActive = a.status === "active";
    const isSelected = state.filter.agent === a.id;
    if (isSelected) li.classList.add("selected");
    const color = AGENT_COLORS[idx % AGENT_COLORS.length];
    li.innerHTML = `
      <div class="title-row">
        <span class="status-dot ${isActive ? "active" : ""}"></span>
        <span class="agent-swatch" style="background:${color}"></span>
        <span class="title">${escapeHTML(a.name || a.agent_type || a.id.slice(0, 8))}</span>
      </div>
    `;
    li.addEventListener("click", () => toggleAgentFilter(a.id));
    els.agentList.appendChild(li);
  });
}

// toggleAgentFilter mirrors the TUI: clicking an agent filters events to
// that agent only; clicking the already-selected agent clears the filter.
async function toggleAgentFilter(agentID) {
  state.filter.agent = state.filter.agent === agentID ? "" : agentID;
  // loadEvents bumps `gen` itself, so any older/newer fetch still in flight
  // for the previous filter is dropped on arrival.
  renderAgents();
  await loadEvents();
}

// loadEvents fetches the latest page (tail) and resets the pager. Used on
// session change, filter change, and the initial render. Bumping `gen`
// invalidates any older/newer fetches still in flight from the previous
// state, so their responses are dropped instead of corrupting the new list.
async function loadEvents() {
  if (!state.sessionId) return;
  state.gen++;
  const myGen = state.gen;
  // Reset loader flags so a stale older/newer fetch returning later cannot
  // wedge `loadingOlder/loadingNewer` in true forever after we bail.
  state.pager.loadingOlder = false;
  state.pager.loadingNewer = false;
  const data = await fetchEventsPage({ tail: true });
  if (myGen !== state.gen) return;
  state.cache.events = data.events || [];
  state.pager.offset = data.offset || 0;
  state.pager.total = data.total || 0;
  state.pager.autoFollow = true;
  renderEvents();
  // Wait for the layout to settle before pinning the scroll to the bottom
  // so users land on the newest event, the same place the TUI starts at.
  requestAnimationFrame(scrollEventsToBottom);
}

// loadOlder prepends one page of older events when the user scrolls near
// the top of the list.
async function loadOlder() {
  const p = state.pager;
  if (p.loadingOlder || p.offset <= 0) return;
  p.loadingOlder = true;
  const myGen = state.gen;
  try {
    const newOffset = Math.max(0, p.offset - p.pageSize);
    const data = await fetchEventsPage({ offset: newOffset, limit: p.offset - newOffset });
    if (myGen !== state.gen) return;
    if (!data.events || data.events.length === 0) return;
    // Preserve the visual position of the topmost visible event by
    // measuring scrollHeight before/after the prepend.
    const scrollEl = els.eventList;
    const heightBefore = scrollEl.scrollHeight;
    const topBefore = scrollEl.scrollTop;
    state.cache.events = [...data.events, ...state.cache.events];
    state.pager.offset = data.offset || newOffset;
    state.pager.total = data.total || p.total;
    renderEvents();
    const heightAfter = scrollEl.scrollHeight;
    scrollEl.scrollTop = topBefore + (heightAfter - heightBefore);
  } finally {
    p.loadingOlder = false;
  }
}

// loadNewer fetches events with offset past our window's tail. Called on
// the auto-refresh tick so newly arriving events show up at the bottom.
//
// We do NOT short-circuit on a cached `pager.total` — the cached value is
// what's stale, by definition, when called from the refresh tick. Instead
// we always issue the fetch and let an empty `events` array signal "no
// new events". The server returns the fresh total in the response.
async function loadNewer() {
  const p = state.pager;
  if (p.loadingNewer) return;
  p.loadingNewer = true;
  const myGen = state.gen;
  try {
    const tailOffset = p.offset + state.cache.events.length;
    const data = await fetchEventsPage({ offset: tailOffset, limit: p.pageSize });
    if (myGen !== state.gen) return;
    // Even when no new events arrive, refresh the total so the count badge
    // doesn't drift (e.g. if events were cleared on the server).
    state.pager.total = data.total ?? p.total;
    if (!data.events || data.events.length === 0) {
      renderEvents();
      return;
    }
    state.cache.events = [...state.cache.events, ...data.events];
    renderEvents();
    if (p.autoFollow) requestAnimationFrame(scrollEventsToBottom);
  } finally {
    p.loadingNewer = false;
  }
}

// fetchEventsPage centralises the URL/query construction so all three
// loaders (initial / older / newer) stay in sync on filter handling.
async function fetchEventsPage({ tail, offset, limit }) {
  const params = new URLSearchParams();
  if (state.filter.type) params.set("type", state.filter.type);
  if (state.filter.search) params.set("search", state.filter.search);
  if (state.filter.agent) params.set("agent", state.filter.agent);
  params.set("limit", String(limit ?? state.pager.pageSize));
  if (tail) params.set("tail", "1");
  else if (offset != null) params.set("offset", String(offset));
  return fetchJSON(`/api/sessions/${state.sessionId}/events?${params.toString()}`);
}

function scrollEventsToBottom() {
  if (els.eventList) els.eventList.scrollTop = els.eventList.scrollHeight;
}

function isScrolledNearBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 40;
}

function isScrolledNearTop(el) {
  return el.scrollTop < 80;
}

function eventTypeClass(ev) {
  const t = (ev.type || "").toLowerCase();
  if (t === "codechange") return "code";
  return t;
}

function renderEvents() {
  els.eventList.innerHTML = "";
  if (els.eventsCount) {
    // pager.total is the filter-aware count from the server; the loaded
    // length may be smaller while the user pages older events. The badge
    // shows "(loaded/total)" when they diverge so the gap is visible.
    const total = state.pager.total || 0;
    const loaded = state.cache.events.length;
    if (total === 0 && loaded === 0) {
      els.eventsCount.textContent = "";
    } else if (loaded === total || total === 0) {
      els.eventsCount.textContent = `(${loaded})`;
    } else {
      els.eventsCount.textContent = `(${loaded}/${total})`;
    }
  }
  state.cache.events.forEach((ev) => {
    const li = document.createElement("li");
    li.dataset.id = ev.id;
    if (ev.id === state.eventId) li.classList.add("selected");
    const cls = eventTypeClass(ev);
    const brief = ev.brief || "";
    const briefCls = "ev-brief" + (ev.highlighted ? " bright" : "");
    const agent = agentLabelFor(ev);
    const agentSpan = agent
      ? `<span class="ev-agent" style="color:${agent.color}" title="${escapeHTML(agent.title)}">${escapeHTML(agent.label)}</span>`
      : `<span class="ev-agent ev-agent-empty"></span>`;
    li.innerHTML = `
      <span class="ev-type ${cls}">${escapeHTML(ev.subtype || ev.type)}</span>
      ${agentSpan}
      <span class="ev-tool">${escapeHTML(ev.tool_name || "")}</span>
      <span class="${briefCls}">${escapeHTML(brief)}</span>
      <span class="ev-time">${fmtTime(ev.timestamp)}</span>
    `;
    li.addEventListener("click", () => selectEvent(ev.id));
    els.eventList.appendChild(li);
  });
}

// Stable color palette for subagent labels — mirrors how the TUI colors
// agent slots so each subagent stays the same hue across renders.
const AGENT_COLORS = [
  "#7ee787", "#79c0ff", "#d2a8ff", "#ffa657", "#f0a3ff",
  "#a5d6ff", "#ffdfb6", "#a8d8b9", "#f8c6e1", "#bcdbff",
];

function agentLabelFor(ev) {
  if (!ev.agent_id) return null;
  const map = state.cache.agentsByID;
  const info = map ? map.get(ev.agent_id) : null;
  if (!info) {
    return { label: ev.agent_id.slice(0, 8), color: "var(--text-dim)", title: ev.agent_id };
  }
  // Root agent (session itself) has no parent and usually no name. Render a
  // dim "root" placeholder so the column lines up but visually recedes.
  const isRoot = !info.parent_agent_id;
  if (isRoot && !info.name && !info.agent_type) {
    return { label: "root", color: "var(--text-dim)", title: info.id };
  }
  const display = info.name || info.agent_type || info.id.slice(0, 8);
  const color = AGENT_COLORS[info._index % AGENT_COLORS.length];
  const title = info.agent_type ? `${display} (${info.agent_type})` : display;
  return { label: truncateLabel(display, 14), color, title };
}

function truncateLabel(s, max) {
  if (!s) return "";
  return s.length <= max ? s : s.slice(0, max - 1) + "…";
}

async function selectEvent(id) {
  // Clicking the already-selected event again closes the detail pane.
  if (state.eventId === id) {
    state.eventId = null;
    state.detailEvent = null;
    state.detailShowJSON = false;
    renderEvents();
    renderEventDetail();
    return;
  }
  state.eventId = id;
  state.detailShowJSON = false;
  renderEvents();
  const ev = await fetchJSON(`/api/events/${id}`);
  // Bail out if the user clicked away while we were fetching.
  if (state.eventId !== id) return;
  state.detailEvent = ev;
  renderEventDetail();
}

// Status string mirrors model.DeriveEventStatus on the server side: this lets
// the web detail header display the same running/completed/failed/pending
// labels the TUI shows.
function deriveStatus(subtype) {
  switch (subtype) {
    case "PreToolUse":
      return { label: "● running", cls: "status-running" };
    case "PostToolUse":
      return { label: "✓ completed", cls: "status-completed" };
    case "PostToolUseFailure":
      return { label: "✗ failed", cls: "status-failed" };
    default:
      return { label: "○ pending", cls: "status-pending" };
  }
}

// toolInput finds the tool argument map regardless of runtime.
// Claude stores it under "tool_input"; OpenCode under "args".
function toolInput(payload) {
  if (!payload) return {};
  if (payload.tool_input && typeof payload.tool_input === "object") return payload.tool_input;
  if (payload.args && typeof payload.args === "object") return payload.args;
  return {};
}

function toolResponse(payload) {
  if (!payload) return null;
  return payload.tool_response ?? null;
}

// Detail sections for known tools. Each renderer returns an array of
// [label, value, kind] triples. `kind` is "field" for inline strings or
// "block" for code blocks rendered in <pre>.
function detailSectionsFor(ev) {
  const payload = ev.payload || null;
  const input = toolInput(payload);
  const response = toolResponse(payload);

  const sections = [];

  // Subtype-specific overrides take precedence over tool-name routing,
  // mirroring the TUI's renderSubtypeDetail / renderKnownToolDetail order.
  switch (ev.subtype) {
    case "UserPromptSubmit":
      pushBlock(sections, "Prompt", str(payload?.prompt));
      return sections;
    case "Stop":
    case "SubagentStop":
      pushBlock(sections, "Last assistant message", str(payload?.last_assistant_message));
      return sections;
    case "Notification":
      pushField(sections, "Message", str(payload?.message));
      pushField(sections, "Permission", str(payload?.permission));
      return sections;
    case "SessionStart":
      pushField(sections, "Model", str(payload?.model));
      pushField(sections, "Source", str(payload?.source));
      return sections;
    case "SessionEnd":
      pushField(sections, "Reason", str(payload?.reason));
      return sections;
    case "PartUpdated": {
      const partType = str(payload?.part_type);
      pushField(sections, "Part", partType);
      if (partType === "text" || partType === "reasoning") {
        pushBlock(sections, "Text", str(payload?.text));
      } else if (partType === "tool") {
        pushField(sections, "Tool", str(payload?.tool_name));
        pushField(sections, "Status", str(payload?.tool_status));
        pushField(sections, "Title", str(payload?.tool_title));
      } else if (partType === "step-finish") {
        pushField(sections, "Tokens in", str(payload?.tokens_input));
        pushField(sections, "Tokens out", str(payload?.tokens_output));
      }
      return sections;
    }
  }

  // Tool-name routing: covers PreToolUse and PostToolUse for known tools.
  switch (ev.tool_name) {
    case "Bash":
      pushField(sections, "Command", str(input.command));
      pushField(sections, "Description", str(input.description));
      pushBlock(sections, "Stdout", str(response?.stdout));
      pushBlock(sections, "Stderr", str(response?.stderr));
      break;

    case "Read": {
      const filePath = str(input.file_path || input.filePath);
      pushField(sections, "File", filePath);
      pushField(sections, "Offset", str(input.offset));
      pushField(sections, "Limit", str(input.limit));
      pushBlock(sections, "Content", str(typeof response === "string" ? response : response?.content));
      break;
    }

    case "Edit": {
      const filePath = str(input.file_path || input.filePath || ev.diff_path);
      pushField(sections, "File", filePath);
      pushStructuredDiff(sections, "Diff", ev);
      break;
    }

    case "Write": {
      const filePath = str(input.file_path || input.filePath);
      pushField(sections, "File", filePath);
      pushBlock(sections, "Content", str(input.content));
      break;
    }

    case "apply_patch":
      pushStructuredDiff(sections, "Patch", ev);
      break;

    case "Grep":
      pushField(sections, "Pattern", str(input.pattern));
      pushField(sections, "Path", str(input.path));
      pushField(sections, "Glob", str(input.glob));
      pushBlock(sections, "Output", responseString(response));
      break;

    case "Glob":
      pushField(sections, "Pattern", str(input.pattern));
      pushField(sections, "Path", str(input.path));
      pushBlock(sections, "Output", responseString(response));
      break;

    case "Agent":
      pushField(sections, "Subagent", str(input.subagent_type));
      pushField(sections, "Description", str(input.description));
      pushBlock(sections, "Prompt", str(input.prompt));
      pushBlock(sections, "Result", responseString(response));
      break;

    default:
      // Generic fallback: show non-empty input fields and the raw response.
      Object.entries(input).forEach(([k, v]) => {
        const sv = str(v);
        if (!sv) return;
        if (sv.includes("\n") || sv.length > 80) {
          pushBlock(sections, capitalize(k), sv);
        } else {
          pushField(sections, capitalize(k), sv);
        }
      });
      const respStr = responseString(response);
      if (respStr) pushBlock(sections, "Response", respStr);
      break;
  }

  return sections;
}

function pushField(sections, label, value) {
  if (!value) return;
  sections.push({ kind: "field", label, value });
}

function pushBlock(sections, label, value) {
  if (!value) return;
  sections.push({ kind: "block", label, value });
}

// pushStructuredDiff consumes the diff_lines / diff_stats fields the server
// pre-computes for Edit and apply_patch events. The renderer below maps
// each Op into the same +/-/  prefixes the TUI uses, with hunk gaps shown
// as a magenta separator line.
function renderDiffLines(lines) {
  // Mirrors the TUI: "- " (red) / "+ " (green) / "  " (dim) prefixes, plus
  // a magenta "~~~" separator for collapsed hunks. Equal lines render with
  // a two-space prefix so the columns line up under the +/- gutter.
  const rows = lines.map((dl) => {
    const text = escapeHTML(dl.text || "");
    switch (dl.op) {
      case "insert":
        return `<div class="diff-row diff-insert"><span class="diff-prefix">+</span> ${text}</div>`;
      case "delete":
        return `<div class="diff-row diff-delete"><span class="diff-prefix">-</span> ${text}</div>`;
      case "gap":
        return `<div class="diff-row diff-gap">${text ? escapeHTML(text) : "~~~"}</div>`;
      default:
        return `<div class="diff-row diff-equal"><span class="diff-prefix">&nbsp;</span> ${text}</div>`;
    }
  });
  return `<div class="diff-block">${rows.join("")}</div>`;
}

function pushStructuredDiff(sections, label, ev) {
  const lines = ev.diff_lines || [];
  if (lines.length === 0) return;
  const stats = ev.diff_stats;
  let header = label;
  if (stats && (stats.additions || stats.deletions)) {
    header += ` (+${stats.additions} -${stats.deletions})`;
  }
  sections.push({ kind: "diff-structured", label: header, lines });
}

function responseString(response) {
  if (response == null) return "";
  if (typeof response === "string") return response;
  if (typeof response === "object") {
    return str(response.stdout) || str(response.output) || JSON.stringify(response, null, 2);
  }
  return String(response);
}

function str(v) {
  if (v == null) return "";
  if (typeof v === "string") return v;
  return String(v);
}

function capitalize(s) {
  if (!s) return "";
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function renderEventDetail() {
  const ev = state.detailEvent;
  // The whole detail pane is hidden via the layout's `no-detail` class when
  // nothing is selected; previously chosen column widths remain stored in
  // CSS vars so opening detail again restores the user's resize.
  const layout = document.getElementById("layout");
  if (layout) layout.classList.toggle("no-detail", !ev);
  if (!ev) {
    els.eventDetail.textContent = "";
    return;
  }
  els.eventDetail.classList.remove("muted");

  const status = deriveStatus(ev.subtype);
  const header = `
    <dl class="meta">
      <dt>Agent</dt><dd>${escapeHTML(ev.agent_id || "—")}</dd>
      <dt>Type</dt><dd>${escapeHTML(ev.type)}${ev.subtype ? ` / ${escapeHTML(ev.subtype)}` : ""}</dd>
      <dt>Tool</dt><dd>${escapeHTML(ev.tool_name || "—")}</dd>
      <dt>Time</dt><dd>${fmtDate(ev.timestamp)}</dd>
      <dt>Status</dt><dd class="${status.cls}">${escapeHTML(status.label)}</dd>
    </dl>
  `;

  const sections = detailSectionsFor(ev);
  let body = "";
  if (sections.length === 0) {
    body = `<div class="detail-empty">no structured fields for this event</div>`;
  } else {
    body = sections
      .map((s) => {
        if (s.kind === "field") {
          return `<div class="detail-field"><span class="dlabel">${escapeHTML(s.label)}:</span> <span class="dvalue">${escapeHTML(s.value)}</span></div>`;
        }
        if (s.kind === "diff-structured") {
          return `<div class="detail-block"><div class="dlabel">${escapeHTML(s.label)}:</div>${renderDiffLines(s.lines)}</div>`;
        }
        const cls = s.kind === "diff" ? "payload diff" : "payload";
        return `<div class="detail-block"><div class="dlabel">${escapeHTML(s.label)}:</div><pre class="${cls}">${escapeHTML(s.value)}</pre></div>`;
      })
      .join("");
  }

  let raw = "";
  if (state.detailShowJSON) {
    let payloadStr = ev.raw || "";
    if (ev.payload != null) {
      try {
        payloadStr = JSON.stringify(ev.payload, null, 2);
      } catch (_) {
        payloadStr = String(ev.payload);
      }
    }
    raw = `<div class="detail-block"><div class="dlabel">Raw JSON:</div><pre class="payload">${escapeHTML(payloadStr)}</pre></div>`;
  }

  const toggle = `<button class="json-toggle" type="button" data-action="toggle-json">${state.detailShowJSON ? "hide raw json" : "show raw json"}</button>`;

  els.eventDetail.innerHTML = `${header}${body}${raw}<div class="detail-actions">${toggle}</div>`;
  const btn = els.eventDetail.querySelector('[data-action="toggle-json"]');
  if (btn) {
    btn.addEventListener("click", () => {
      state.detailShowJSON = !state.detailShowJSON;
      renderEventDetail();
    });
  }
}

function escapeHTML(s) {
  if (s == null) return "";
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

async function refreshAll() {
  try {
    await loadProjects();
    if (state.sessionId) {
      // Auto-refresh appends new events at the tail rather than reloading
      // the whole window — preserves scroll position when the user is
      // browsing older events, mirroring the TUI's behavior.
      await Promise.all([loadAgents(), loadNewer()]);
      renderSessionMeta();
    }
  } catch (err) {
    console.error("refresh failed", err);
  }
}

function setupEvents() {
  els.refreshBtn.addEventListener("click", refreshAll);
  els.filterType.addEventListener("change", () => {
    state.filter.type = els.filterType.value;
    loadEvents();
  });
  let searchTimer = null;
  els.filterSearch.addEventListener("input", () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      state.filter.search = els.filterSearch.value.trim();
      loadEvents();
    }, 250);
  });

  // Track whether the user is "tailing" the events list. Anchored to the
  // bottom → autoFollow on, refresh ticks pin scroll. Scrolled away → off,
  // refresh ticks still append but leave scroll where the user put it.
  // Scrolling near the top while older events exist triggers a prepend.
  if (els.eventList) {
    els.eventList.addEventListener("scroll", () => {
      const el = els.eventList;
      state.pager.autoFollow = isScrolledNearBottom(el);
      if (isScrolledNearTop(el)) loadOlder();
    });
  }

  if (els.usageBtn) els.usageBtn.addEventListener("click", openUsage);
  if (els.usageModal) {
    els.usageModal.addEventListener("click", (e) => {
      if (e.target.dataset?.action === "close-usage") closeUsage();
    });
  }
  document.addEventListener("keydown", (e) => {
    // Ignore key presses while typing in inputs so the search box stays usable.
    const tag = (e.target?.tagName || "").toLowerCase();
    if (tag === "input" || tag === "textarea" || tag === "select") return;
    if (e.key === "b" && state.sessionId) {
      e.preventDefault();
      openUsage();
    } else if (e.key === "Escape") {
      closeUsage();
    }
  });
}

async function openUsage() {
  if (!state.sessionId) return;
  els.usageModal.classList.remove("hidden");
  els.usageModal.setAttribute("aria-hidden", "false");
  els.usageBody.classList.add("muted");
  els.usageBody.textContent = "loading…";
  try {
    const data = await fetchJSON(`/api/sessions/${state.sessionId}/usage`);
    renderUsage(data);
  } catch (err) {
    els.usageBody.classList.remove("muted");
    els.usageBody.innerHTML = `<div class="detail-empty">failed to load usage: ${escapeHTML(String(err))}</div>`;
  }
}

function closeUsage() {
  els.usageModal.classList.add("hidden");
  els.usageModal.setAttribute("aria-hidden", "true");
}

function fmtNum(n) {
  if (n == null) return "0";
  return Number(n).toLocaleString();
}

function fmtCost(n) {
  if (!n) return "$0.00";
  return "$" + Number(n).toFixed(4);
}

function renderUsage(data) {
  els.usageBody.classList.remove("muted");
  // Session header is always shown so the user sees which run the numbers
  // belong to even when the loader returned an error or no API calls.
  const sessionHeader = renderUsageSessionHeader();
  if (!data || (!data.usage && data.error)) {
    els.usageBody.innerHTML =
      sessionHeader +
      `<div class="detail-empty">${escapeHTML(data?.error || "no token data available")}</div>`;
    return;
  }
  if (!data.usage) {
    els.usageBody.innerHTML = sessionHeader + `<div class="detail-empty">no token data available</div>`;
    return;
  }

  const u = data.usage;
  const t = u.tokens || {};
  const directIn = t.input_tokens || 0;
  const cacheRead = t.cache_read_tokens || 0;
  const cacheWrite = t.cache_creation_tokens || 0;
  const out = t.output_tokens || 0;
  const totalIn = directIn + cacheRead + cacheWrite;
  const cacheShare = totalIn > 0 ? ((cacheRead / totalIn) * 100).toFixed(1) + "%" : "—";
  const outRatio = directIn > 0 ? (out / directIn).toFixed(2) : "—";

  const overview = `
    <dl class="usage-grid">
      <dt>Cost</dt><dd>${fmtCost(u.cost_usd)}</dd>
      <dt>Model Calls</dt><dd>${fmtNum(u.api_calls)}</dd>
      <dt>Direct Input</dt><dd>${fmtNum(directIn)}</dd>
      <dt>Total Input</dt><dd>${fmtNum(totalIn)}</dd>
      <dt>Output</dt><dd>${fmtNum(out)}</dd>
      <dt>Cache Read</dt><dd>${fmtNum(cacheRead)}</dd>
      <dt>Cache Write</dt><dd>${fmtNum(cacheWrite)}</dd>
      <dt>Cache Share</dt><dd>${cacheShare}</dd>
      <dt>Output Ratio</dt><dd>${outRatio}</dd>
    </dl>
  `;

  const models = (u.model_breakdown || [])
    .map(
      (m) => `
      <tr>
        <td>${escapeHTML(m.model)}</td>
        <td class="num">${fmtNum(m.calls)}</td>
        <td class="num">${fmtNum((m.tokens?.input_tokens || 0) + (m.tokens?.cache_read_tokens || 0) + (m.tokens?.cache_creation_tokens || 0))}</td>
        <td class="num">${fmtNum(m.tokens?.output_tokens)}</td>
        <td class="num">${fmtCost(m.cost_usd)}</td>
      </tr>`
    )
    .join("");

  const tools = (u.tool_breakdown || [])
    .map((t) => `<tr><td>${escapeHTML(t.name)}</td><td class="num">${fmtNum(t.calls)}</td></tr>`)
    .join("");

  const cmds = (u.bash_breakdown || [])
    .map((t) => `<tr><td>${escapeHTML(t.name)}</td><td class="num">${fmtNum(t.calls)}</td></tr>`)
    .join("");

  const sectionTable = (title, headers, rows) => {
    if (!rows) return "";
    return `
      <div class="usage-section">
        <h4>${escapeHTML(title)}</h4>
        <table class="usage-table">
          <thead><tr>${headers.map((h, i) => `<th class="${i === 0 ? "" : "num"}">${escapeHTML(h)}</th>`).join("")}</tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  };

  els.usageBody.innerHTML =
    sessionHeader +
    overview +
    sectionTable("Model Ledger", ["Model", "Calls", "Total In", "Output", "Cost"], models) +
    sectionTable("Tools", ["Tool", "Calls"], tools) +
    sectionTable("Shell Commands", ["Command", "Calls"], cmds);
}

function renderUsageSessionHeader() {
  const session = findSession(state.sessionId);
  if (!session) return "";
  const title = session.slug || session.id.slice(0, 8);
  const lastLabel = session.status === "stopped" && session.stopped_at
    ? `stopped ${fmtDate(session.stopped_at)}`
    : session.last_activity
      ? `last activity ${fmtDate(session.last_activity)}`
      : "";
  return `
    <div class="usage-session">
      <div class="usage-session-title">
        <span class="runtime-tag">${runtimeTag(session.runtime)}</span>
        <span class="meta-title">${escapeHTML(title)}</span>
        ${session.project_name ? `<span class="usage-session-project">${escapeHTML(session.project_name)}</span>` : ""}
      </div>
      <dl class="usage-grid usage-session-grid">
        <dt>Session ID</dt><dd class="meta-id">${escapeHTML(session.id)}</dd>
        <dt>Status</dt><dd>${escapeHTML(session.status || "—")}</dd>
        <dt>Started</dt><dd>${fmtDate(session.started_at)}</dd>
        <dt>${lastLabel ? "Last" : ""}</dt><dd>${escapeHTML(lastLabel || "")}</dd>
        <dt>Events</dt><dd>${fmtNum(session.event_count)}</dd>
        <dt>Agents</dt><dd>${fmtNum(session.agent_count)}</dd>
      </dl>
    </div>
  `;
}

function startAutoRefresh() {
  setInterval(() => {
    if (els.autoRefresh.checked) refreshAll();
  }, REFRESH_MS);
}

setupSplitters();
setupCollapsibles();
setupEvents();
refreshAll();
startAutoRefresh();

// setupCollapsibles makes the Agents and Events headers click-to-toggle.
// State persists in localStorage so the chosen layout survives reloads.
function setupCollapsibles() {
  const STORAGE_KEY = "lazyagent.layout.collapsed";
  let collapsed = {};
  try {
    collapsed = JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}") || {};
  } catch (_) {}

  document.querySelectorAll("h3.collapsible").forEach((h) => {
    const targetId = h.dataset.toggle;
    const block = document.getElementById(targetId);
    if (!block) return;
    const startCollapsed = !!collapsed[targetId];
    setCollapsed(block, h, startCollapsed);
    h.addEventListener("click", () => {
      const next = !block.classList.contains("collapsed");
      setCollapsed(block, h, next);
      collapsed[targetId] = next;
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(collapsed));
      } catch (_) {}
    });
  });
}

function setCollapsed(block, header, isCollapsed) {
  block.classList.toggle("collapsed", isCollapsed);
  header.setAttribute("aria-expanded", isCollapsed ? "false" : "true");
}

// setupSplitters wires up drag-to-resize for the two vertical dividers
// between the projects/middle/detail panes. Widths are stored as CSS custom
// properties on the layout root and persisted to localStorage so the
// chosen layout survives reloads.
function setupSplitters() {
  const layout = document.getElementById("layout");
  if (!layout) return;
  const STORAGE_KEY = "lazyagent.layout.cols";
  const MIN_PROJECTS = 160;
  const MIN_MIDDLE = 240;
  const MIN_DETAIL = 240;
  const SPLITTER_PX = 6;

  // Restore saved widths.
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || "null");
    if (saved && typeof saved.projects === "number" && typeof saved.middle === "number") {
      layout.style.setProperty("--col-projects", saved.projects + "px");
      layout.style.setProperty("--col-middle", saved.middle + "px");
    }
  } catch (_) {}

  const splitters = layout.querySelectorAll(".splitter");
  splitters.forEach((sp) => {
    sp.addEventListener("pointerdown", (e) => beginDrag(e, sp));
  });

  function currentCols() {
    const total = layout.clientWidth;
    const computed = getComputedStyle(layout).gridTemplateColumns.split(" ").map((v) => parseFloat(v));
    const noDetail = layout.classList.contains("no-detail");
    // Expected layout: [projects, splitter, middle, splitter, detail]
    // or with no-detail:  [projects, splitter, middle]
    let projects = computed[0] || MIN_PROJECTS;
    let middle = computed[2] || MIN_MIDDLE;
    // Guard against negatives during early renders (total can briefly be
    // 0 before the layout engine has resolved the grid).
    let detail = noDetail ? 0 : Math.max(0, total - projects - middle - SPLITTER_PX * 2);
    return { projects, middle, detail, total, noDetail };
  }

  function beginDrag(e, sp) {
    e.preventDefault();
    const which = sp.dataset.splitter; // "0" | "1"
    const start = currentCols();
    const startX = e.clientX;
    sp.classList.add("active");
    layout.classList.add("dragging");
    sp.setPointerCapture?.(e.pointerId);

    const onMove = (ev) => {
      const dx = ev.clientX - startX;
      let projects = start.projects;
      let middle = start.middle;
      let detail = start.detail;

      if (which === "0") {
        // Left splitter: move projects/middle boundary. When detail is
        // hidden the splitter count drops to 1 and the middle pane fills
        // whatever remains; otherwise detail width stays untouched.
        if (start.noDetail) {
          const maxProjects = start.total - SPLITTER_PX - MIN_MIDDLE;
          projects = clamp(start.projects + dx, MIN_PROJECTS, maxProjects);
          middle = start.total - projects - SPLITTER_PX;
        } else {
          projects = clamp(start.projects + dx, MIN_PROJECTS, start.total - SPLITTER_PX * 2 - MIN_MIDDLE - MIN_DETAIL);
          middle = start.total - projects - detail - SPLITTER_PX * 2;
          if (middle < MIN_MIDDLE) {
            middle = MIN_MIDDLE;
            projects = start.total - middle - detail - SPLITTER_PX * 2;
          }
        }
      } else {
        // Right splitter: move middle/detail boundary; projects is unchanged.
        middle = clamp(start.middle + dx, MIN_MIDDLE, start.total - SPLITTER_PX * 2 - projects - MIN_DETAIL);
        detail = start.total - projects - middle - SPLITTER_PX * 2;
      }

      layout.style.setProperty("--col-projects", projects + "px");
      layout.style.setProperty("--col-middle", middle + "px");
    };

    const onUp = (ev) => {
      sp.classList.remove("active");
      layout.classList.remove("dragging");
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
      sp.releasePointerCapture?.(ev?.pointerId ?? e.pointerId);
      try {
        const final = currentCols();
        localStorage.setItem(
          STORAGE_KEY,
          JSON.stringify({ projects: Math.round(final.projects), middle: Math.round(final.middle) })
        );
      } catch (_) {}
    };

    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
  }

  function clamp(v, lo, hi) {
    return Math.max(lo, Math.min(hi, v));
  }
}
