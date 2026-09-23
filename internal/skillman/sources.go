package skillman

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, cwd, name string, args ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, cwd, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func GHQRoot(ctx context.Context, runner Runner) (string, error) {
	output, err := runner.Run(ctx, "", "ghq", "root")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(output)
	if root == "" {
		return "", errors.New("ghq returned an empty root")
	}
	return filepath.EvalSymlinks(root)
}

func EnsureSources(ctx context.Context, rootRepo, ghqRoot string, update bool, runner Runner) error {
	visited := make(map[string]bool)
	return ensureCollectionSources(ctx, rootRepo, ghqRoot, update, runner, visited)
}

func ensureCollectionSources(ctx context.Context, collectionPath, ghqRoot string, update bool, runner Runner, visited map[string]bool) error {
	realPath, err := filepath.EvalSymlinks(collectionPath)
	if err != nil {
		return err
	}
	if visited[realPath] {
		return nil
	}
	visited[realPath] = true

	manifest, err := readUpstreamManifest(filepath.Join(realPath, "upstream-sources.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, source := range manifest.Sources {
		repoID := repositoryGHQID(source.Repository)
		repoPath := filepath.Join(ghqRoot, filepath.FromSlash(repoID))
		if _, err := os.Stat(repoPath); errors.Is(err, os.ErrNotExist) {
			if _, err := runner.Run(ctx, "", "ghq", "get", "-p", repoID); err != nil {
				return fmt.Errorf("clone %s: %w", source.Repository, err)
			}
		} else if err != nil {
			return err
		}

		if update {
			if err := updateSource(ctx, repoPath, normalizeRef(source.Ref), runner); err != nil {
				return fmt.Errorf("update %s: %w", source.Repository, err)
			}
		}
		if _, err := os.Stat(filepath.Join(repoPath, "upstream-sources.json")); err == nil {
			if err := ensureCollectionSources(ctx, repoPath, ghqRoot, update, runner, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

func SyncRepository(ctx context.Context, repoPath, ref string, runner Runner) error {
	return updateSource(ctx, repoPath, normalizeRef(ref), runner)
}

func updateSource(ctx context.Context, repoPath, ref string, runner Runner) error {
	status, err := runner.Run(ctx, repoPath, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("working tree is dirty; commit or stash before syncing")
	}
	if _, err := runner.Run(ctx, repoPath, "git", "fetch", "origin", "--prune"); err != nil {
		return err
	}
	if ref == "" {
		return nil
	}
	branch, err := runner.Run(ctx, repoPath, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != ref {
		if _, err := runner.Run(ctx, repoPath, "git", "switch", ref); err != nil {
			return err
		}
	}
	_, err = runner.Run(ctx, repoPath, "git", "pull", "--ff-only", "origin", ref)
	return err
}

func repositoryGHQID(repository string) string {
	if strings.HasPrefix(repository, "github.com/") {
		return repository
	}
	return "github.com/" + repository
}
