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

type SkillState struct {
	Enabled bool `json:"enabled"`
	Linked  bool `json:"linked"`
	Partial bool `json:"partial"`
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

func (m *Manager) SkillState(skill Skill) (SkillState, error) {
	enabled := 0
	linked := 0
	for _, harness := range m.Harnesses {
		state, err := m.State(skill, harness)
		if err != nil {
			return SkillState{}, err
		}
		if state.Enabled {
			enabled++
		}
		if state.Enabled && state.Linked {
			linked++
		}
	}
	allEnabled := len(m.Harnesses) > 0 && enabled == len(m.Harnesses)
	return SkillState{
		Enabled: allEnabled,
		Linked:  allEnabled && linked == len(m.Harnesses),
		Partial: enabled > 0 && !allEnabled,
	}, nil
}

func (m *Manager) Enable(ctx context.Context, skill Skill) error {
	for _, harness := range m.Harnesses {
		if _, err := m.Runner.Run(ctx, m.RepoRoot, "npx", "--yes", "skills", "add", skill.InstallSource(), "--skill", skill.Name, "--global", "--agent", harness.AgentID, "--yes"); err != nil {
			_, _ = m.Runner.Run(ctx, m.RepoRoot, "npx", "--yes", "skills", "remove", skill.Name, "--global", "--yes")
			return err
		}
	}
	for _, harness := range m.Harnesses {
		if err := replaceWithSymlink(filepath.Join(harness.Path, skill.Name), skill.Directory); err != nil {
			return err
		}
	}
	return SetLocalLockSkill(m.RepoRoot, skill, true)
}

func (m *Manager) Disable(ctx context.Context, skill Skill) error {
	installed := false
	for _, harness := range m.Harnesses {
		state, err := m.State(skill, harness)
		if err != nil {
			return err
		}
		installed = installed || state.Enabled
	}
	if installed {
		if _, err := m.Runner.Run(ctx, m.RepoRoot, "npx", "--yes", "skills", "remove", skill.Name, "--global", "--yes"); err != nil {
			return err
		}
	}
	for _, harness := range m.Harnesses {
		if err := removeManagedPath(filepath.Join(harness.Path, skill.Name)); err != nil {
			return err
		}
	}
	return SetLocalLockSkill(m.RepoRoot, skill, false)
}

func (m *Manager) Reconcile(catalog Catalog) (int, error) {
	linked := 0
	enabled := make([]Skill, 0)
	for _, skill := range catalog.Skills {
		installed := false
		states := make(map[string]InstallState, len(m.Harnesses))
		for _, harness := range m.Harnesses {
			state, err := m.State(skill, harness)
			if err != nil {
				return linked, err
			}
			states[harness.ID] = state
			installed = installed || state.Enabled
		}
		if !installed {
			continue
		}
		for _, harness := range m.Harnesses {
			if states[harness.ID].Enabled && states[harness.ID].Linked {
				continue
			}
			if err := replaceWithSymlink(filepath.Join(harness.Path, skill.Name), skill.Directory); err != nil {
				return linked, err
			}
			linked++
		}
		enabled = append(enabled, skill)
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
