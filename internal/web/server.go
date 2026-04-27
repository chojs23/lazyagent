// Package web serves a read-only HTTP dashboard for lazyagent.
//
// The web UI mirrors what the TUI shows but in a browser. It reuses the same
// SQLite store as the source of truth, so sessions, agents, and events stay in
// sync without any extra writer path. Endpoints are intentionally read-only:
// the server only exposes GET handlers that wrap existing store.Queries calls.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/model"
	"github.com/chojs23/lazyagent/internal/store"
	"github.com/chojs23/lazyagent/internal/tokens"
)

//go:embed static
var staticFS embed.FS

// Options controls how the web server is bound.
type Options struct {
	Host string
	Port int
}

// Run starts the HTTP server and blocks until ctx is cancelled. The store
// pointer is shared with any other consumer (TUI, ingest stream); handlers
// only call read methods so concurrent access is safe.
func Run(ctx context.Context, st *store.Store, opts Options) error {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 7777
	}

	mux := http.NewServeMux()
	registerAPI(mux, st)

	// Serve embedded static files at "/". The embed FS exposes assets under
	// the "static" prefix, so strip that before fs.Sub returns a subtree
	// rooted at the index.html parent.
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("web: load static fs: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))

	addr := net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           withLogging(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("lazyagent web UI: http://%s\n", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				applog.Error("web: handler panic", fmt.Errorf("%v", rec))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// registerAPI wires the read-only JSON endpoints. Routes use plain prefix
// matching to avoid pulling in a router dependency.
func registerAPI(mux *http.ServeMux, st *store.Store) {
	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		projects, err := st.Read().ListProjects(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"projects": projectsView(projects)})
	})

	// /api/projects/{id}/sessions
	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
		parts := strings.Split(rest, "/")
		if len(parts) != 2 || parts[1] != "sessions" {
			http.NotFound(w, r)
			return
		}
		projectID, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid project id", http.StatusBadRequest)
			return
		}
		sessions, err := st.Read().ListSessionsForProject(r.Context(), projectID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]any{"sessions": sessionsView(sessions)})
	})

	// /api/sessions/{id}             -> session metadata
	// /api/sessions/{id}/agents      -> agent tree
	// /api/sessions/{id}/events      -> events (with filter query params)
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		parts := strings.SplitN(rest, "/", 2)
		sessionID := parts[0]
		if sessionID == "" {
			http.NotFound(w, r)
			return
		}

		switch {
		case len(parts) == 1:
			session, err := st.Read().GetSessionByID(r.Context(), sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			if session == nil {
				http.NotFound(w, r)
				return
			}
			writeJSON(w, sessionView(*session))

		case parts[1] == "agents":
			agents, err := st.Read().ListAgentsForSessionTree(r.Context(), sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, map[string]any{"agents": agentsView(agents)})

		case parts[1] == "events":
			filter := parseEventFilter(r)
			// Total is the filter-aware count of all matching events. The
			// frontend uses it to know when older events exist and to size
			// the count badge correctly under filters.
			total, err := st.Read().CountFilteredEventsForSessionTree(r.Context(), sessionID, filter)
			if err != nil {
				writeError(w, err)
				return
			}
			// `tail=1` means "give me the latest page": clamp offset so that
			// it returns the most recent `limit` events. This is what the
			// frontend uses on initial load and on filter changes.
			if r.URL.Query().Get("tail") == "1" && filter.Limit > 0 {
				if total > filter.Limit {
					filter.Offset = total - filter.Limit
				} else {
					filter.Offset = 0
				}
			}
			events, err := st.Read().ListEventsForSessionTree(r.Context(), sessionID, filter)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, map[string]any{
				"events": eventsView(events),
				"total":  total,
				"offset": filter.Offset,
			})

		case parts[1] == "usage":
			session, err := st.Read().GetSessionByID(r.Context(), sessionID)
			if err != nil {
				writeError(w, err)
				return
			}
			if session == nil {
				http.NotFound(w, r)
				return
			}
			summary, err := tokens.For(r.Context(), st, session)
			// A loader error is not fatal: it usually means the transcript
			// is unavailable. Surface it in the response so the UI can
			// render a banner instead of a 500.
			resp := map[string]any{}
			if err != nil {
				resp["error"] = err.Error()
			}
			if summary != nil && summary.APICalls > 0 {
				resp["usage"] = usageView(summary)
			}
			writeJSON(w, resp)

		default:
			http.NotFound(w, r)
		}
	})

	// /api/events/{id} -> single event with full payload
	mux.HandleFunc("/api/events/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/events/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid event id", http.StatusBadRequest)
			return
		}
		ev, err := st.Read().GetEventByID(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		if ev == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, eventDetailView(*ev))
	})
}

// parseEventFilter pulls EventFilter fields from URL query parameters. The
// limit defaults to 500 to keep payload sizes bounded; the TUI typically
// shows fewer than that on screen at a time.
func parseEventFilter(r *http.Request) model.EventFilter {
	q := r.URL.Query()
	f := model.EventFilter{
		Type:    q.Get("type"),
		Subtype: q.Get("subtype"),
		Search:  q.Get("search"),
	}
	if agent := q.Get("agent"); agent != "" {
		f.AgentIDs = []string{agent}
	}
	if limit, err := strconv.Atoi(q.Get("limit")); err == nil && limit > 0 {
		f.Limit = limit
	} else {
		f.Limit = 500
	}
	if offset, err := strconv.Atoi(q.Get("offset")); err == nil && offset > 0 {
		f.Offset = offset
	}
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Default escaping (escapes `<`, `>`, `&` in JSON strings) stays on as
	// a defense-in-depth layer: the frontend uses escapeHTML before
	// inserting into the DOM, but a single missed call would otherwise
	// open an XSS vector via event payloads.
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		applog.Error("web: encode json", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	applog.Error("web: handler", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
