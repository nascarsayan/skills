package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nascarsayan/skills/internal/skillman"
)

type skillItem struct {
	skill  skillman.Skill
	states map[string]skillman.InstallState
	active string
}

func (i skillItem) Title() string {
	shared := stateMark(i.states["universal"], i.active == "universal")
	claude := stateMark(i.states["claude-code"], i.active == "claude-code")
	return fmt.Sprintf("U%s C%s  %s  [%s@%s]", shared, claude, i.skill.Name, i.skill.Repository, displayRef(i.skill.Ref))
}

func (i skillItem) Description() string { return i.skill.Description }
func (i skillItem) FilterValue() string {
	return i.skill.Name + " " + i.skill.Repository + " " + i.skill.Description
}

func stateMark(state skillman.InstallState, active bool) string {
	mark := "·"
	if state.Enabled && state.Linked {
		mark = "✓"
	} else if state.Enabled {
		mark = "!"
	}
	if active {
		return "[" + mark + "]"
	}
	return " " + mark + " "
}

func displayRef(ref string) string {
	if ref == "" {
		return "default"
	}
	return ref
}

type operationDone struct {
	message string
	err     error
	reload  bool
}

type tuiModel struct {
	catalog  skillman.Catalog
	manager  *skillman.Manager
	list     list.Model
	active   int
	busy     bool
	status   string
	repoRoot string
	ghqRoot  string
}

func newTUIModel(catalog skillman.Catalog, manager *skillman.Manager, repoRoot, ghqRoot string) (tuiModel, error) {
	delegate := list.NewDefaultDelegate()
	model := tuiModel{
		catalog:  catalog,
		manager:  manager,
		active:   0,
		repoRoot: repoRoot,
		ghqRoot:  ghqRoot,
	}
	items, err := model.items()
	if err != nil {
		return tuiModel{}, err
	}
	model.list = list.New(items, delegate, 120, 32)
	model.list.Title = "skillman"
	model.list.SetShowStatusBar(true)
	model.list.SetFilteringEnabled(true)
	model.list.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	return model, nil
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(message.Width, max(8, message.Height-3))
	case operationDone:
		m.busy = false
		if message.err != nil {
			m.status = "error: " + message.err.Error()
		} else {
			m.status = message.message
		}
		if message.reload && message.err == nil {
			catalog, err := skillman.LoadCatalog(m.repoRoot, m.ghqRoot)
			if err != nil {
				m.status = "error: " + err.Error()
			} else {
				m.catalog = catalog
			}
		}
		if err := m.refreshItems(); err != nil {
			m.status = "error: " + err.Error()
		}
	}

	if key, ok := message.(tea.KeyMsg); ok && m.list.FilterState() == list.Unfiltered {
		if m.busy && key.String() != "ctrl+c" && key.String() != "q" {
			return m, nil
		}
		switch key.String() {
		case "tab", "right", "left":
			m.active = (m.active + 1) % len(m.manager.Harnesses)
			m.status = "active harness: " + m.manager.Harnesses[m.active].Name
			_ = m.refreshItems()
			return m, nil
		case " ":
			item, ok := m.selectedItem()
			if !ok {
				return m, nil
			}
			harness := m.manager.Harnesses[m.active]
			m.busy = true
			m.status = "updating " + item.skill.Name
			return m, runOperation(func() error {
				if item.states[harness.ID].Enabled {
					return m.manager.Disable(context.Background(), item.skill, harness)
				}
				return m.manager.Enable(context.Background(), item.skill, harness)
			}, "updated "+item.skill.Name, false)
		case "a":
			item, ok := m.selectedItem()
			if !ok {
				return m, nil
			}
			m.busy = true
			return m, runOperation(func() error {
				for _, harness := range m.manager.Harnesses {
					state, err := m.manager.State(item.skill, harness)
					if err != nil {
						return err
					}
					if !state.Enabled {
						if err := m.manager.Enable(context.Background(), item.skill, harness); err != nil {
							return err
						}
					}
				}
				return nil
			}, "enabled "+item.skill.Name+" everywhere", false)
		case "d":
			item, ok := m.selectedItem()
			if !ok {
				return m, nil
			}
			m.busy = true
			return m, runOperation(func() error {
				for i := len(m.manager.Harnesses) - 1; i >= 0; i-- {
					harness := m.manager.Harnesses[i]
					state, err := m.manager.State(item.skill, harness)
					if err != nil {
						return err
					}
					if state.Enabled {
						if err := m.manager.Disable(context.Background(), item.skill, harness); err != nil {
							return err
						}
					}
				}
				return nil
			}, "disabled "+item.skill.Name+" everywhere", false)
		case "r":
			item, ok := m.selectedItem()
			if !ok {
				return m, nil
			}
			m.busy = true
			return m, runOperation(func() error {
				return skillman.SyncRepository(context.Background(), item.skill.RepoPath, item.skill.Ref, m.manager.Runner)
			}, "synced "+item.skill.Repository, true)
		case "R":
			m.busy = true
			return m, runOperation(func() error {
				return skillman.EnsureSources(context.Background(), m.repoRoot, m.ghqRoot, true, m.manager.Runner)
			}, "synced all upstream sources", true)
		}
	}

	var command tea.Cmd
	m.list, command = m.list.Update(message)
	return m, command
}

func (m tuiModel) View() string {
	active := m.manager.Harnesses[m.active].Name
	busy := ""
	if m.busy {
		busy = " | busy"
	}
	footer := fmt.Sprintf("active: %s%s | space toggle | tab harness | a all on | d all off | r sync source | R sync all | / filter", active, busy)
	if m.status != "" {
		footer += "\n" + truncate(m.status, 140)
	}
	return m.list.View() + "\n" + footer
}

func (m tuiModel) selectedItem() (skillItem, bool) {
	item, ok := m.list.SelectedItem().(skillItem)
	return item, ok
}

func (m tuiModel) items() ([]list.Item, error) {
	items := make([]list.Item, 0, len(m.catalog.Skills))
	for _, skill := range m.catalog.Skills {
		states := make(map[string]skillman.InstallState, len(m.manager.Harnesses))
		for _, harness := range m.manager.Harnesses {
			state, err := m.manager.State(skill, harness)
			if err != nil {
				return nil, err
			}
			states[harness.ID] = state
		}
		items = append(items, skillItem{skill: skill, states: states, active: m.manager.Harnesses[m.active].ID})
	}
	return items, nil
}

func (m *tuiModel) refreshItems() error {
	index := m.list.Index()
	items, err := m.items()
	if err != nil {
		return err
	}
	command := m.list.SetItems(items)
	if command != nil {
		_ = command
	}
	if len(items) > 0 {
		m.list.Select(min(index, len(items)-1))
	}
	return nil
}

func runOperation(operation func() error, success string, reload bool) tea.Cmd {
	return func() tea.Msg {
		return operationDone{message: success, err: operation(), reload: reload}
	}
}

func truncate(value string, limit int) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\n", " ")
	if len(value) <= limit {
		return value
	}
	return value[:limit-1] + "…"
}
