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

func TestReconcileAndTogglePreserveGitBackedSource(t *testing.T) {
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
	for _, harness := range harnesses {
		mustWriteSkill(t, filepath.Join(harness.Path, "demo"), "demo", "legacy copy")
	}

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
	if linked != 2 {
		t.Fatalf("linked %d paths, want 2", linked)
	}
	for _, harness := range harnesses {
		assertDirectLink(t, filepath.Join(harness.Path, "demo"), skillDir)
	}
	lock, err := ReadLocalLock(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if entry := lock.Skills["demo"]; entry.Source != "owner/skills" || entry.Ref != "dev" {
		t.Fatalf("unexpected lock entry: %+v", entry)
	}

	if err := manager.Disable(context.Background(), skill, harnesses[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(harnesses[1].Path, "demo")); !os.IsNotExist(err) {
		t.Fatalf("Claude link still exists: %v", err)
	}
	assertDirectLink(t, filepath.Join(harnesses[0].Path, "demo"), skillDir)
	if len(runner.calls) != 0 {
		t.Fatalf("partial disable should not call npx: %+v", runner.calls)
	}

	if err := manager.Disable(context.Background(), skill, harnesses[0]); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0].name != "npx" || !strings.Contains(strings.Join(runner.calls[0].args, " "), "skills remove demo") {
		t.Fatalf("last disable did not use npx remove: %+v", runner.calls)
	}
	lock, err = ReadLocalLock(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Skills) != 0 {
		t.Fatalf("lock still contains disabled skills: %+v", lock.Skills)
	}

	runner.calls = nil
	if err := manager.Enable(context.Background(), skill, harnesses[1]); err != nil {
		t.Fatal(err)
	}
	assertDirectLink(t, filepath.Join(harnesses[1].Path, "demo"), skillDir)
	if _, err := os.Lstat(filepath.Join(harnesses[0].Path, "demo")); !os.IsNotExist(err) {
		t.Fatalf("universal link created for Claude-only enablement: %v", err)
	}
	if len(runner.calls) != 1 || !strings.Contains(strings.Join(runner.calls[0].args, " "), "skills add owner/skills#dev") {
		t.Fatalf("enable did not use npx add: %+v", runner.calls)
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
