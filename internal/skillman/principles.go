package skillman

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type PrincipleTarget struct {
	Harness string
	Path    string
}

func DefaultPrincipleTargets(home string) []PrincipleTarget {
	return []PrincipleTarget{
		{Harness: "OMP", Path: filepath.Join(home, ".omp", "agent", "RULES.md")},
		{Harness: "Claude Code", Path: filepath.Join(home, ".claude", "CLAUDE.md")},
		{Harness: "Codex", Path: filepath.Join(home, ".codex", "AGENTS.md")},
		{Harness: "GitHub Copilot", Path: filepath.Join(home, ".copilot", "copilot-instructions.md")},
		{Harness: "OpenCode", Path: filepath.Join(home, ".config", "opencode", "AGENTS.md")},
		{Harness: "Pi", Path: filepath.Join(home, ".pi", "agent", "AGENTS.md")},
	}
}

func ReconcilePrinciples(source string, targets []PrincipleTarget) (int, error) {
	desired, err := filepath.EvalSymlinks(source)
	if err != nil {
		return 0, fmt.Errorf("resolve principles source: %w", err)
	}
	sourceContent, err := os.ReadFile(desired)
	if err != nil {
		return 0, err
	}

	linked := 0
	for _, target := range targets {
		matches, err := linkMatches(target.Path, desired)
		if err != nil {
			return linked, err
		}
		if matches {
			continue
		}
		info, err := os.Lstat(target.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return linked, err
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return linked, fmt.Errorf("refusing to replace %s principles symlink %s", target.Harness, target.Path)
			}
			current, readErr := os.ReadFile(target.Path)
			if readErr != nil {
				return linked, readErr
			}
			if string(current) != string(sourceContent) {
				return linked, fmt.Errorf("refusing to replace non-matching %s instructions at %s", target.Harness, target.Path)
			}
			if err := os.Remove(target.Path); err != nil {
				return linked, err
			}
		}
		if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
			return linked, err
		}
		if err := os.Symlink(desired, target.Path); err != nil {
			return linked, err
		}
		linked++
	}
	return linked, nil
}

func CheckPrinciples(source string, targets []PrincipleTarget) error {
	for _, target := range targets {
		matches, err := linkMatches(target.Path, source)
		if err != nil {
			return err
		}
		if !matches {
			return fmt.Errorf("%s instructions are not linked to %s", target.Harness, source)
		}
	}
	return nil
}
