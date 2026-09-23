package skillman

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSourceSwitchesToConfiguredRef(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	work := filepath.Join(root, "work")

	runGit(t, root, "init", "--bare", origin)
	runGit(t, root, "init", "-b", "main", seed)
	runGit(t, seed, "config", "user.email", "skillman@example.com")
	runGit(t, seed, "config", "user.name", "skillman test")
	if err := os.WriteFile(filepath.Join(seed, "branch.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "branch.txt")
	runGit(t, seed, "commit", "-m", "main")
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "origin", "main")
	runGit(t, seed, "switch", "-c", "dev")
	if err := os.WriteFile(filepath.Join(seed, "branch.txt"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "commit", "-am", "dev")
	runGit(t, seed, "push", "origin", "dev")
	runGit(t, root, "clone", "--branch", "main", origin, work)

	if err := updateSource(context.Background(), work, "dev", ExecRunner{}); err != nil {
		t.Fatal(err)
	}
	branch := strings.TrimSpace(runGit(t, work, "branch", "--show-current"))
	if branch != "dev" {
		t.Fatalf("branch is %q, want dev", branch)
	}
	content, err := os.ReadFile(filepath.Join(work, "branch.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "dev\n" {
		t.Fatalf("branch content is %q", content)
	}

	if err := os.WriteFile(filepath.Join(work, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := updateSource(context.Background(), work, "main", ExecRunner{}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty checkout should be rejected, got %v", err)
	}
}

func runGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = cwd
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
