package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chojs23/lazyagent/internal/store"
)

const maxProjectSlugSuffix = 1000

func resolveProject(ctx context.Context, q *store.Queries, transcriptPath, cwd string) (int64, error) {
	if cwd != "" {
		proj, err := q.GetProjectByDirectory(ctx, cwd)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			return proj.ID, nil
		}

		candidates := deriveSlugCandidates(cwd)
		projectBySlug, foundBySlug, err := findProjectBySlugCandidates(ctx, q, candidates)
		if err != nil {
			return 0, err
		}
		if foundBySlug {
			if projectBySlug.Directory == "" {
				q.UpdateProjectDirectory(ctx, projectBySlug.ID, cwd)
			}
			return projectBySlug.ID, nil
		}

		return createProjectFromCandidates(ctx, q, candidates, cwd, "")
	}

	if transcriptPath != "" {
		transcriptDir := extractProjectDir(transcriptPath)
		proj, err := q.GetProjectByTranscriptPath(ctx, transcriptDir)
		if err != nil {
			return 0, err
		}
		if proj != nil {
			if cwd != "" && proj.Directory == "" {
				q.UpdateProjectDirectory(ctx, proj.ID, cwd)
			}
			return proj.ID, nil
		}

		candidates := deriveSlugCandidates(transcriptPath)
		return createProjectFromCandidates(ctx, q, candidates, cwd, transcriptDir)
	}

	proj, err := q.GetProjectBySlug(ctx, "unknown")
	if err != nil {
		return 0, err
	}
	if proj != nil {
		return proj.ID, nil
	}
	return q.CreateProject(ctx, "unknown", "unknown", "", "")
}

func findProjectBySlugCandidates(ctx context.Context, q *store.Queries, candidates []string) (projectLookup, bool, error) {
	for _, c := range candidates {
		proj, err := q.GetProjectBySlug(ctx, c)
		if err != nil {
			return projectLookup{}, false, err
		}
		if proj != nil {
			return projectLookup{ID: proj.ID, Directory: proj.Directory}, true, nil
		}
	}
	return projectLookup{}, false, nil
}

type projectLookup struct {
	ID        int64
	Directory string
}

func createProjectFromCandidates(ctx context.Context, q *store.Queries, candidates []string, directory, transcriptPath string) (int64, error) {
	for _, c := range candidates {
		avail, err := q.IsSlugAvailable(ctx, c)
		if err != nil {
			return 0, err
		}
		if avail {
			return q.CreateProject(ctx, c, c, directory, transcriptPath)
		}
	}

	return createProjectWithUniqueSlug(ctx, q, candidates[0], directory, transcriptPath)
}

func createProjectWithUniqueSlug(ctx context.Context, q *store.Queries, base, directory, transcriptPath string) (int64, error) {
	for suffix := 2; suffix <= maxProjectSlugSuffix; suffix++ {
		slug := fmt.Sprintf("%s-%d", base, suffix)
		avail, err := q.IsSlugAvailable(ctx, slug)
		if err != nil {
			return 0, err
		}
		if avail {
			return q.CreateProject(ctx, slug, slug, directory, transcriptPath)
		}
	}

	return 0, fmt.Errorf("resolve project slug: exhausted suffixes for %q up to %d", base, maxProjectSlugSuffix)
}

func extractProjectDir(transcriptPath string) string {
	cleaned := strings.TrimRight(transcriptPath, "/")
	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		return cleaned
	}
	if ext := filepath.Ext(cleaned); ext != "" {
		return filepath.Dir(cleaned)
	}
	return cleaned
}

func deriveSlugCandidates(pathOrDir string) []string {
	dir := extractProjectDir(pathOrDir)
	encoded := filepath.Base(dir)
	var parts []string
	for _, p := range strings.Split(encoded, "-") {
		if p != "" {
			parts = append(parts, strings.ToLower(p))
		}
	}
	if len(parts) == 0 {
		return []string{"unknown"}
	}
	if len(parts) == 1 {
		return []string{parts[0]}
	}

	var candidates []string
	seen := make(map[string]struct{})
	addCandidate := func(candidate string) {
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	addCandidate(strings.Join(parts[max(0, len(parts)-2):], "-"))
	addCandidate(parts[len(parts)-1])
	for start := len(parts) - 3; start >= 0; start-- {
		addCandidate(strings.Join(parts[start:], "-"))
	}
	return candidates
}
