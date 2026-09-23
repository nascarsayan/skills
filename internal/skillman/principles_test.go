package skillman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcilePrinciplesLinksMissingAndMatchingFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "catalog", "PRINCIPLES.md")
	content := []byte("# Principles\n\n- Keep changes small.\n")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}
	targets := []PrincipleTarget{
		{Harness: "One", Path: filepath.Join(root, "one", "AGENTS.md")},
		{Harness: "Two", Path: filepath.Join(root, "two", "RULES.md")},
	}
	if err := os.MkdirAll(filepath.Dir(targets[1].Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targets[1].Path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	linked, err := ReconcilePrinciples(source, targets)
	if err != nil {
		t.Fatal(err)
	}
	if linked != len(targets) {
		t.Fatalf("linked %d paths, want %d", linked, len(targets))
	}
	if err := CheckPrinciples(source, targets); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePrinciplesRefusesDifferentInstructions(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "PRINCIPLES.md")
	target := filepath.Join(root, "harness", "AGENTS.md")
	if err := os.WriteFile(source, []byte("canonical"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReconcilePrinciples(source, []PrincipleTarget{{Harness: "Test", Path: target}})
	if err == nil || !strings.Contains(err.Error(), "refusing to replace non-matching") {
		t.Fatalf("unexpected error: %v", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "local" {
		t.Fatalf("conflicting instructions changed to %q", content)
	}
}
