package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chojs23/lazyagent/internal/store"
)

func TestIngestCrossRuntimeProjectUnification(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	claudeResult, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "claude-sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-projects-lazyagent2/session.jsonl",
		"cwd":             "/home/user/projects/lazyagent2",
		"meta":            map[string]any{"timestamp": float64(1712700000000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	ocResult, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-sess-1",
		"project_dir": "/home/user/projects/lazyagent2",
		"title":       "main",
		"timestamp":   float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	if claudeResult.ProjectID != ocResult.ProjectID {
		t.Fatalf("project IDs differ: claude=%d opencode=%d", claudeResult.ProjectID, ocResult.ProjectID)
	}
}

func TestIngestCrossRuntimeProjectUnificationOpenCodeFirst(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	ocResult, err := IngestOpenCodeEvent(ctx, st, map[string]any{
		"event":       "session.created",
		"session_id":  "opencode-sess-1",
		"project_dir": "/home/user/projects/myapp",
		"title":       "main",
		"timestamp":   float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	claudeResult, err := IngestClaudeEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "claude-sess-1",
		"transcript_path": "/home/user/.claude/projects/-home-user-projects-myapp/session.jsonl",
		"cwd":             "/home/user/projects/myapp",
		"meta":            map[string]any{"timestamp": float64(1712700001000)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if ocResult.ProjectID != claudeResult.ProjectID {
		t.Fatalf("project IDs differ: opencode=%d claude=%d", ocResult.ProjectID, claudeResult.ProjectID)
	}
}

func TestDeriveSlugCandidates(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/home/user/.claude/projects/-home-user-my-app/s.jsonl", "my-app"},
		{"/home/user/.claude/projects/-Users-my-new-project/s.jsonl", "new-project"},
		{"/x/-a/s.jsonl", "a"},
	}
	for _, tc := range cases {
		candidates := deriveSlugCandidates(tc.input)
		if len(candidates) == 0 {
			t.Fatalf("no candidates for %q", tc.input)
		}
		if candidates[0] != tc.want {
			t.Fatalf("deriveSlugCandidates(%q)[0] = %q, want %q", tc.input, candidates[0], tc.want)
		}
	}
}

func TestExtractProjectDir(t *testing.T) {
	got := extractProjectDir("/home/user/.claude/projects/-home-user-app/session.jsonl")
	want := "/home/user/.claude/projects/-home-user-app"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractProjectDirKeepsExistingDottedDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "foo.bar")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := extractProjectDir(dir); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestCreateProjectWithUniqueSlugFailsWhenSuffixesAreExhausted(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	err := st.WithTx(ctx, func(q *store.Queries) error {
		for suffix := 2; suffix <= maxProjectSlugSuffix; suffix++ {
			slug := fmt.Sprintf("my-app-%d", suffix)
			if _, err := q.CreateProject(ctx, slug, slug, fmt.Sprintf("/taken/%d", suffix), ""); err != nil {
				return err
			}
		}
		_, err := createProjectWithUniqueSlug(ctx, q, "my-app", "/home/user/my-app", "")
		return err
	})
	if err == nil {
		t.Fatal("expected createProjectWithUniqueSlug to fail after slug suffix exhaustion")
	}
	if want := fmt.Sprintf("resolve project slug: exhausted suffixes for %q up to %d", "my-app", maxProjectSlugSuffix); err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
