package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/list"
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
	assertTUIView(t, model, viewAll, 2, "disabled")
}

func TestTUISortsByNameAndRepositoryAcrossFilteredViews(t *testing.T) {
	root := t.TempDir()
	harnesses := []skillman.Harness{
		{ID: "universal", Path: filepath.Join(root, ".agents", "skills")},
		{ID: "claude-code", Path: filepath.Join(root, ".claude", "skills")},
	}
	zebra := tuiTestSkill(t, root, "zebra")
	zebra.Repository = "alpha/repo"
	alpha := tuiTestSkill(t, root, "alpha")
	alpha.Repository = "zeta/repo"
	beta := tuiTestSkill(t, root, "beta")
	beta.Repository = "alpha/repo"
	skills := []skillman.Skill{zebra, alpha, beta}
	for _, harness := range harnesses {
		if err := os.MkdirAll(harness.Path, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, skill := range skills {
			if err := os.Symlink(skill.Directory, filepath.Join(harness.Path, skill.Name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	manager := &skillman.Manager{Harnesses: harnesses}
	model, err := newTUIModel(skillman.Catalog{Skills: skills}, manager, root, root)
	if err != nil {
		t.Fatal(err)
	}
	assertItemOrder(t, model.list.Items(), []string{"alpha", "beta", "zebra"})

	updated, _ := model.Update(tea.KeyMsg{Runes: []rune{'s'}, Type: tea.KeyRunes})
	model = updated.(tuiModel)
	if model.sort != sortRepository {
		t.Fatalf("sort is %s, want Repository", model.sort)
	}
	assertItemOrder(t, model.list.Items(), []string{"beta", "zebra", "alpha"})

	model.list.SetFilterText("repo")
	assertItemOrder(t, model.list.VisibleItems(), []string{"beta", "zebra", "alpha"})
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	if model.sort != sortRepository || model.list.FilterState() != list.FilterApplied {
		t.Fatalf("sort or filter was lost in Disabled view: sort=%s filter=%s", model.sort, model.list.FilterState())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	assertItemOrder(t, model.list.VisibleItems(), []string{"beta", "zebra", "alpha"})
}

func assertItemOrder(t *testing.T, items []list.Item, expected []string) {
	t.Helper()
	if len(items) != len(expected) {
		t.Fatalf("got %d items, want %d", len(items), len(expected))
	}
	for index, item := range items {
		if name := item.(skillItem).skill.Name; name != expected[index] {
			t.Fatalf("item %d is %s, want %s", index, name, expected[index])
		}
	}
}

type tuiRunner struct{}

func (tuiRunner) Run(context.Context, string, string, ...string) (string, error) {
	return "", nil
}

func TestTUIActionsPreserveAppliedSearchAndEscapeClearsIt(t *testing.T) {
	root := t.TempDir()
	harnesses := []skillman.Harness{
		{ID: "universal", AgentID: "codex", Path: filepath.Join(root, ".agents", "skills")},
		{ID: "claude-code", AgentID: "claude-code", Path: filepath.Join(root, ".claude", "skills")},
	}
	enabled := tuiTestSkill(t, root, "needle-enabled")
	disabled := tuiTestSkill(t, root, "needle-disabled")
	for _, harness := range harnesses {
		if err := os.MkdirAll(harness.Path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(enabled.Directory, filepath.Join(harness.Path, enabled.Name)); err != nil {
			t.Fatal(err)
		}
	}
	manager := &skillman.Manager{RepoRoot: root, Harnesses: harnesses, Runner: tuiRunner{}}
	model, err := newTUIModel(skillman.Catalog{Skills: []skillman.Skill{enabled, disabled}}, manager, root, root)
	if err != nil {
		t.Fatal(err)
	}
	model.list.SetFilterText("needle")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	assertAppliedFilter(t, model, viewDisabled, 1)

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(tuiModel)
	if command == nil {
		t.Fatal("filtered enable did not return a command")
	}
	updated, _ = model.Update(command())
	model = updated.(tuiModel)
	assertAppliedFilter(t, model, viewDisabled, 0)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(tuiModel)
	assertAppliedFilter(t, model, viewAll, 2)
	for index, item := range model.list.VisibleItems() {
		if item.(skillItem).skill.Name == disabled.Name {
			model.list.Select(index)
			break
		}
	}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(tuiModel)
	if command == nil {
		t.Fatal("filtered disable did not return a command")
	}
	updated, _ = model.Update(command())
	model = updated.(tuiModel)
	state, err := manager.SkillState(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.Partial {
		t.Fatalf("skill remained enabled after filtered disable: %+v", state)
	}
	assertAppliedFilter(t, model, viewAll, 2)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(tuiModel)
	if model.list.FilterState() != list.Unfiltered || model.list.FilterInput.Value() != "" {
		t.Fatalf("escape did not clear filter: state=%s value=%q", model.list.FilterState(), model.list.FilterInput.Value())
	}
}

func assertAppliedFilter(t *testing.T, model tuiModel, view viewMode, visible int) {
	t.Helper()
	if model.view != view {
		t.Fatalf("view is %s, want %s", model.view, view)
	}
	if model.list.FilterState() != list.FilterApplied || model.list.FilterInput.Value() != "needle" {
		t.Fatalf("filter was not preserved: state=%s value=%q", model.list.FilterState(), model.list.FilterInput.Value())
	}
	if got := len(model.list.VisibleItems()); got != visible {
		t.Fatalf("view %s has %d filtered items, want %d", view, got, visible)
	}
}

func tuiTestSkill(t *testing.T, root, name string) skillman.Skill {
	t.Helper()
	directory := filepath.Join(root, "sources", name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: test\n---\n"), 0o644); err != nil {
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
