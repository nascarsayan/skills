package skillman

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedCall struct {
	name string
	args []string
}

type recordingRunner struct {
	calls []recordedCall
}

func (r *recordingRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.calls = append(r.calls, recordedCall{name: name, args: append([]string(nil), args...)})
	return "", nil
}

func TestReconcileAndToggleUseEveryHarness(t *testing.T) {
	root := t.TempDir()
	repoRoot := filepath.Join(root, "catalog")
	skillDir := filepath.Join(root, "ghq", "github.com", "owner", "skills", "skills", "demo")
	mustWriteSkill(t, skillDir, "demo", "demo skill")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	harnesses := []Harness{
		{ID: "universal", Name: "Universal", AgentID: "codex", Path: filepath.Join(root, ".agents", "skills")},
		{ID: "claude-code", Name: "Claude", AgentID: "claude-code", Path: filepath.Join(root, ".claude", "skills")},
	}
	mustWriteSkill(t, filepath.Join(harnesses[0].Path, "demo"), "demo", "legacy copy")

	skill := Skill{
		Name:        "demo",
		Description: "demo skill",
		Repository:  "owner/skills",
		URL:         "https://github.com/owner/skills",
		Ref:         "dev",
		SkillPath:   "skills/demo/SKILL.md",
		Directory:   skillDir,
		RepoPath:    filepath.Join(root, "ghq", "github.com", "owner", "skills"),
	}
	runner := &recordingRunner{}
	manager := &Manager{RepoRoot: repoRoot, Harnesses: harnesses, Runner: runner}

	linked, err := manager.Reconcile(Catalog{Skills: []Skill{skill}})
	if err != nil {
		t.Fatal(err)
	}
	if linked != len(harnesses) {
		t.Fatalf("linked %d paths, want %d", linked, len(harnesses))
	}
	for _, harness := range harnesses {
		assertDirectLink(t, filepath.Join(harness.Path, "demo"), skillDir)
	}
	state, err := manager.SkillState(skill)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || !state.Linked || state.Partial {
		t.Fatalf("unexpected reconciled state: %+v", state)
	}

	if err := manager.Disable(context.Background(), skill); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "npx" || !strings.Contains(strings.Join(runner.calls[0].args, " "), "skills remove demo") {
		t.Fatalf("disable did not use one npx remove: %+v", runner.calls)
	}
	for _, harness := range harnesses {
		if _, err := os.Lstat(filepath.Join(harness.Path, "demo")); !os.IsNotExist(err) {
			t.Fatalf("%s link still exists: %v", harness.Name, err)
		}
	}
	lock, err := ReadLocalLock(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Skills) != 0 {
		t.Fatalf("lock still contains disabled skills: %+v", lock.Skills)
	}

	runner.calls = nil
	if err := manager.Enable(context.Background(), skill); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != len(harnesses) {
		t.Fatalf("enable made %d npx calls, want %d: %+v", len(runner.calls), len(harnesses), runner.calls)
	}
	for index, harness := range harnesses {
		assertDirectLink(t, filepath.Join(harness.Path, "demo"), skillDir)
		args := strings.Join(runner.calls[index].args, " ")
		if !strings.Contains(args, "skills add owner/skills#dev") || !strings.Contains(args, "--agent "+harness.AgentID) {
			t.Fatalf("unexpected enable call: %+v", runner.calls[index])
		}
	}
}

func assertDirectLink(t *testing.T, path, target string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", path)
	}
	linkTarget, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != target {
		t.Fatalf("%s points to %s, want %s", path, linkTarget, target)
	}
}
