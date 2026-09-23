package skillman

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Harness struct {
	ID      string
	Name    string
	AgentID string
	Path    string
}

type InstallState struct {
	Enabled bool   `json:"enabled"`
	Linked  bool   `json:"linked"`
	Target  string `json:"target,omitempty"`
}

type Manager struct {
	RepoRoot  string
	Harnesses []Harness
	Runner    Runner
}

func DefaultHarnesses(home string) []Harness {
	return []Harness{
		{
			ID:      "universal",
			Name:    "Universal (OMP, Codex, Copilot, OpenCode, Pi)",
			AgentID: "codex",
			Path:    filepath.Join(home, ".agents", "skills"),
		},
		{
			ID:      "claude-code",
			Name:    "Claude Code",
			AgentID: "claude-code",
			Path:    filepath.Join(home, ".claude", "skills"),
		},
	}
}

func (m *Manager) State(skill Skill, harness Harness) (InstallState, error) {
	path := filepath.Join(harness.Path, skill.Name)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return InstallState{}, nil
		}
		return InstallState{}, err
	}

	state := InstallState{Enabled: true}
	if info.Mode()&os.ModeSymlink == 0 {
		return state, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		state.Target = "broken"
		return state, nil
	}
	state.Target = target
	desired, err := filepath.EvalSymlinks(skill.Directory)
	if err != nil {
		return InstallState{}, err
	}
	state.Linked = target == desired
	return state, nil
}

func (m *Manager) Enable(ctx context.Context, skill Skill, harness Harness) error {
	before := make(map[string]InstallState, len(m.Harnesses))
	for _, configured := range m.Harnesses {
		state, err := m.State(skill, configured)
		if err != nil {
			return err
		}
		before[configured.ID] = state
	}

	if _, err := m.Runner.Run(ctx, m.RepoRoot, "npx", "--yes", "skills", "add", skill.InstallSource(), "--skill", skill.Name, "--global", "--agent", harness.AgentID, "--yes"); err != nil {
		return err
	}

	for _, configured := range m.Harnesses {
		if configured.ID == harness.ID || before[configured.ID].Enabled {
			if err := replaceWithSymlink(filepath.Join(configured.Path, skill.Name), skill.Directory); err != nil {
				return err
			}
		}
	}
	if harness.ID != "universal" && !before["universal"].Enabled {
		if err := removeManagedPath(filepath.Join(harnessByID(m.Harnesses, "universal").Path, skill.Name)); err != nil {
			return err
		}
	}
	return SetLocalLockSkill(m.RepoRoot, skill, true)
}

func (m *Manager) Disable(ctx context.Context, skill Skill, harness Harness) error {
	states := make(map[string]InstallState, len(m.Harnesses))
	remaining := false
	for _, configured := range m.Harnesses {
		state, err := m.State(skill, configured)
		if err != nil {
			return err
		}
		states[configured.ID] = state
		if configured.ID != harness.ID && state.Enabled {
			remaining = true
		}
	}
	if !states[harness.ID].Enabled {
		return nil
	}

	if !remaining {
		if _, err := m.Runner.Run(ctx, m.RepoRoot, "npx", "--yes", "skills", "remove", skill.Name, "--global", "--yes"); err != nil {
			return err
		}
		for _, configured := range m.Harnesses {
			if err := removeManagedPath(filepath.Join(configured.Path, skill.Name)); err != nil {
				return err
			}
		}
		return SetLocalLockSkill(m.RepoRoot, skill, false)
	}

	for _, configured := range m.Harnesses {
		if configured.ID != harness.ID && states[configured.ID].Enabled {
			if err := replaceWithSymlink(filepath.Join(configured.Path, skill.Name), skill.Directory); err != nil {
				return err
			}
		}
	}
	return removeManagedPath(filepath.Join(harness.Path, skill.Name))
}

func (m *Manager) Reconcile(catalog Catalog) (int, error) {
	linked := 0
	enabled := make([]Skill, 0)
	for _, skill := range catalog.Skills {
		isEnabled := false
		for _, harness := range m.Harnesses {
			state, err := m.State(skill, harness)
			if err != nil {
				return linked, err
			}
			if !state.Enabled {
				continue
			}
			isEnabled = true
			if !state.Linked {
				if err := replaceWithSymlink(filepath.Join(harness.Path, skill.Name), skill.Directory); err != nil {
					return linked, err
				}
				linked++
			}
		}
		if isEnabled {
			enabled = append(enabled, skill)
		}
	}
	if err := ReplaceLocalLock(m.RepoRoot, enabled); err != nil {
		return linked, err
	}
	return linked, nil
}

func replaceWithSymlink(path, target string) error {
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		return fmt.Errorf("invalid skill target %s: %w", target, err)
	}
	if state, err := linkMatches(path, target); err != nil {
		return err
	} else if state {
		return nil
	}
	if err := removeManagedPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

func linkMatches(path, target string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, nil
	}
	desired, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false, err
	}
	return resolved == desired, nil
}

func removeManagedPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return os.Remove(path)
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		return fmt.Errorf("refusing to remove non-skill directory %s", path)
	}
	return os.RemoveAll(path)
}

func harnessByID(harnesses []Harness, id string) Harness {
	for _, harness := range harnesses {
		if harness.ID == id {
			return harness
		}
	}
	return Harness{}
}
