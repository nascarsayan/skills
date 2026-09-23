package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nascarsayan/skills/internal/skillman"
)

func TestTUIStartsEnabledAndCyclesViews(t *testing.T) {
	root := t.TempDir()
	harnesses := []skillman.Harness{
		{ID: "universal", Path: filepath.Join(root, ".agents", "skills")},
		{ID: "claude-code", Path: filepath.Join(root, ".claude", "skills")},
	}
	enabled := tuiTestSkill(t, root, "enabled")
	disabled := tuiTestSkill(t, root, "disabled")
	for _, harness := range harnesses {
		if err := os.MkdirAll(harness.Path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(enabled.Directory, filepath.Join(harness.Path, enabled.Name)); err != nil {
			t.Fatal(err)
		}
	}
	manager := &skillman.Manager{Harnesses: harnesses}
	model, err := newTUIModel(skillman.Catalog{Skills: []skillman.Skill{enabled, disabled}}, manager, root, root)
	if err != nil {
		t.Fatal(err)
	}
	assertTUIView(t, model, viewEnabled, 1, "enabled")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	assertTUIView(t, model, viewDisabled, 1, "disabled")

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	assertTUIView(t, model, viewAll, 2, "enabled")
}

func tuiTestSkill(t *testing.T, root, name string) skillman.Skill {
	t.Helper()
	directory := filepath.Join(root, "sources", name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	return skillman.Skill{Name: name, Description: name + " skill", Directory: directory, Repository: "owner/skills", Ref: "main"}
}

func assertTUIView(t *testing.T, model tuiModel, view viewMode, count int, selected string) {
	t.Helper()
	if model.view != view {
		t.Fatalf("view is %s, want %s", model.view, view)
	}
	if got := len(model.list.Items()); got != count {
		t.Fatalf("view %s has %d items, want %d", view, got, count)
	}
	item, ok := model.selectedItem()
	if !ok || item.skill.Name != selected {
		t.Fatalf("selected item is %+v, want %s", item, selected)
	}
}
