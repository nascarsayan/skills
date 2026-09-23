package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nascarsayan/skills/internal/skillman"
)

type skillItem struct {
	skill skillman.Skill
	state skillman.SkillState
}

func (i skillItem) Title() string {
	return fmt.Sprintf("%s  %s  [%s@%s]", stateMark(i.state), i.skill.Name, i.skill.Repository, displayRef(i.skill.Ref))
}

func (i skillItem) Description() string { return i.skill.Description }
func (i skillItem) FilterValue() string {
	return i.skill.Name + " " + i.skill.Repository + " " + i.skill.Description
}

func stateMark(state skillman.SkillState) string {
	if state.Partial || (state.Enabled && !state.Linked) {
		return "!"
	}
	if state.Enabled {
		return "✓"
	}
	return "·"
}

func displayRef(ref string) string {
	if ref == "" {
		return "default"
	}
	return ref
}

type viewMode uint8

const (
	viewEnabled viewMode = iota
	viewDisabled
	viewAll
)

func (v viewMode) String() string {
	switch v {
	case viewDisabled:
		return "Disabled"
	case viewAll:
		return "All"
	default:
		return "Enabled"
	}
}

type sortMode uint8

const (
	sortName sortMode = iota
	sortRepository
)

func (s sortMode) String() string {
	if s == sortRepository {
		return "Repository"
	}
	return "Name"
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
	view     viewMode
	sort     sortMode
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
		view:     viewEnabled,
		repoRoot: repoRoot,
		ghqRoot:  ghqRoot,
	}
	items, err := model.items()
	if err != nil {
		return tuiModel{}, err
	}
	model.list = list.New(items, delegate, 120, 32)
	model.list.Title = "skillman"
	model.list.Filter = list.UnsortedFilter
	model.list.SetShowStatusBar(true)
	model.list.SetFilteringEnabled(true)
	model.list.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	model.setListTitle(len(items))
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

	if key, ok := message.(tea.KeyMsg); ok && m.list.FilterState() != list.Filtering {
		if m.busy && key.String() != "ctrl+c" && key.String() != "q" {
			return m, nil
		}
		switch key.String() {
		case "tab":
			m.view = (m.view + 1) % 3
			m.status = "view: " + m.view.String()
			_ = m.refreshItems()
			return m, nil
		case "s":
			m.sort = (m.sort + 1) % 2
			m.status = "sort: " + m.sort.String()
			_ = m.refreshItems()
			return m, nil
		case " ":
			item, ok := m.selectedItem()
			if !ok {
				return m, nil
			}
			m.busy = true
			if item.state.Enabled || item.state.Partial {
				m.status = "disabling " + item.skill.Name
				return m, runOperation(func() error {
					return m.manager.Disable(context.Background(), item.skill)
				}, "disabled "+item.skill.Name, false)
			}
			m.status = "enabling " + item.skill.Name
			return m, runOperation(func() error {
				return m.manager.Enable(context.Background(), item.skill)
			}, "enabled "+item.skill.Name, false)
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
	busy := ""
	if m.busy {
		busy = " | busy"
	}
	footer := fmt.Sprintf("view: %s | sort: %s%s | space toggle | tab view | s sort | r sync source | R sync all | / search", m.view, m.sort, busy)
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
	skills := make([]skillItem, 0, len(m.catalog.Skills))
	for _, skill := range m.catalog.Skills {
		state, err := m.manager.SkillState(skill)
		if err != nil {
			return nil, err
		}
		if m.view == viewEnabled && !state.Enabled && !state.Partial {
			continue
		}
		if m.view == viewDisabled && (state.Enabled || state.Partial) {
			continue
		}
		skills = append(skills, skillItem{skill: skill, state: state})
	}
	sort.SliceStable(skills, func(left, right int) bool {
		a, b := skills[left].skill, skills[right].skill
		if m.sort == sortRepository {
			aRepo, bRepo := strings.ToLower(a.Repository), strings.ToLower(b.Repository)
			if aRepo != bRepo {
				return aRepo < bRepo
			}
		}
		aName, bName := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if aName != bName {
			return aName < bName
		}
		return a.Name < b.Name
	})
	items := make([]list.Item, len(skills))
	for index := range skills {
		items[index] = skills[index]
	}
	return items, nil
}

func (m *tuiModel) refreshItems() error {
	index := m.list.Index()
	filterApplied := m.list.FilterState() == list.FilterApplied
	filterText := m.list.FilterInput.Value()
	items, err := m.items()
	if err != nil {
		return err
	}
	_ = m.list.SetItems(items)
	if filterApplied {
		m.list.SetFilterText(filterText)
	} else if len(items) > 0 {
		m.list.Select(min(index, len(items)-1))
	}
	m.setListTitle(len(items))
	return nil
}

func (m *tuiModel) setListTitle(count int) {
	m.list.Title = fmt.Sprintf("skillman · %s (%d)", m.view, count)
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
