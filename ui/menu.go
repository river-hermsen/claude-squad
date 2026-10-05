package ui

import (
	"claude-squad/keys"
	"slices"
	"strings"

	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
)

var keyStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#655F5F",
	Dark:  "#7F7A7A",
})

var descStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#7A7474",
	Dark:  "#9C9494",
})

var sepStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#DDDADA",
	Dark:  "#3C3C3C",
})

var actionGroupStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))

var separator = " • "
var verticalSeparator = " │ "

var menuStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("205"))

// MenuState represents different states the menu can be in
type MenuState int

const (
	StateDefault MenuState = iota
	StateEmpty
	StateNewInstance
	StatePrompt
)

type Menu struct {
	options []keys.KeyName
	// groupEnds holds the index of the last option of each group; groups are separated by
	// vertical bars. Options in [actionStart, actionEnd) are highlighted.
	groupEnds              []int
	actionStart, actionEnd int
	height, width          int
	state                  MenuState
	instance               *session.Instance
	activeTab              int
	// focusMode is true while keys go to the selected session; see SetFocusMode.
	focusMode bool

	// keyDown is the key which is pressed. The default is -1.
	keyDown keys.KeyName
}

var defaultMenuGroups = [][]keys.KeyName{{keys.KeyNew, keys.KeyPrompt, keys.KeyResumeClaude}, {keys.KeyHelp, keys.KeyQuit}}
var newInstanceMenuOptions = []keys.KeyName{keys.KeySubmitName}
var promptMenuOptions = []keys.KeyName{keys.KeySubmitName}

func NewMenu() *Menu {
	m := &Menu{
		state:     StateEmpty,
		activeTab: 0,
		keyDown:   -1,
	}
	m.setGroups(0, defaultMenuGroups...)
	return m
}

// setGroups sets the options from groups of keys. The group at index actionGroup is
// highlighted; pass -1 for none. Empty groups are skipped.
func (m *Menu) setGroups(actionGroup int, groups ...[]keys.KeyName) {
	m.options, m.groupEnds = nil, nil
	m.actionStart, m.actionEnd = 0, 0
	for i, group := range groups {
		if len(group) == 0 {
			continue
		}
		if i == actionGroup {
			m.actionStart, m.actionEnd = len(m.options), len(m.options)+len(group)
		}
		m.options = append(m.options, group...)
		m.groupEnds = append(m.groupEnds, len(m.options)-1)
	}
}

func (m *Menu) Keydown(name keys.KeyName) {
	m.keyDown = name
}

func (m *Menu) ClearKeydown() {
	m.keyDown = -1
}

// SetState updates the menu state and options accordingly
func (m *Menu) SetState(state MenuState) {
	m.state = state
	m.updateOptions()
}

// SetInstance updates the current instance and refreshes menu options
func (m *Menu) SetInstance(instance *session.Instance) {
	m.instance = instance
	// Only change the state if we're not in a special state (NewInstance or Prompt)
	if m.state != StateNewInstance && m.state != StatePrompt {
		if m.instance != nil {
			m.state = StateDefault
		} else {
			m.state = StateEmpty
		}
	}
	m.updateOptions()
}

// SetFocusMode makes the menu, while on is true, only say that keys go to the selected
// session and how to stop.
func (m *Menu) SetFocusMode(on bool) {
	m.focusMode = on
}

// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}

// updateOptions updates the menu options based on current state and instance
func (m *Menu) updateOptions() {
	switch m.state {
	case StateEmpty:
		m.setGroups(0, defaultMenuGroups...)
	case StateDefault:
		if m.instance != nil {
			// When there is an instance, show that instance's options
			m.addInstanceOptions()
		} else {
			// When there is no instance, show the empty state
			m.setGroups(0, defaultMenuGroups...)
		}
	case StateNewInstance:
		m.setGroups(-1, newInstanceMenuOptions)
	case StatePrompt:
		m.setGroups(-1, promptMenuOptions)
	}
}

func (m *Menu) addInstanceOptions() {
	// Loading instances only get minimal options
	if m.instance != nil && m.instance.Status == session.Loading {
		m.setGroups(-1, []keys.KeyName{keys.KeyNew}, []keys.KeyName{keys.KeyHelp, keys.KeyQuit})
		return
	}

	// Instance management group. Forking continues a Claude Code conversation.
	managementGroup := []keys.KeyName{keys.KeyNew, keys.KeyResumeClaude, keys.KeyKill, keys.KeyRename}
	if m.instance.IsClaude() {
		managementGroup = append(managementGroup, keys.KeyFork)
	}

	// Action group. Checkout needs a git worktree.
	actionGroup := []keys.KeyName{keys.KeyEnter}
	if m.instance.Status == session.Paused {
		actionGroup = append(actionGroup, keys.KeyResume)
	} else {
		actionGroup = append(actionGroup, keys.KeyFocus)
		if !m.instance.InPlace() {
			actionGroup = append(actionGroup, keys.KeyCheckout)
		}
	}

	// Navigation group
	var navigationGroup []keys.KeyName
	if m.activeTab == DiffTab || m.activeTab == TerminalTab {
		navigationGroup = append(navigationGroup, keys.KeyShiftUp)
	}
	if m.activeTab == DiffTab && m.instance.IsMultiRepo() {
		if stats := m.instance.GetDiffStats(); stats != nil && len(stats.Repos) > 1 {
			navigationGroup = append(navigationGroup, keys.KeyNextRepo)
		}
	}
	navigationGroup = append(navigationGroup, keys.KeyTab)

	// System group
	systemGroup := []keys.KeyName{keys.KeyHelp, keys.KeyQuit}

	m.setGroups(1, managementGroup, actionGroup, navigationGroup, systemGroup)
}

// SetSize sets the width of the window. The menu will be centered horizontally within this width.
func (m *Menu) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Menu) String() string {
	var s strings.Builder

	if m.focusMode && m.instance != nil {
		exit := keys.GlobalkeyBindings[keys.KeyFocusExit].Help()
		s.WriteString(descStyle.Render("typing into " + m.instance.Title))
		s.WriteString(sepStyle.Render(verticalSeparator))
		s.WriteString(actionGroupStyle.Render(exit.Key + " " + exit.Desc))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, menuStyle.Render(s.String()))
	}

	for i, k := range m.options {
		binding := keys.GlobalkeyBindings[k]

		var (
			localActionStyle = actionGroupStyle
			localKeyStyle    = keyStyle
			localDescStyle   = descStyle
		)
		if m.keyDown == k {
			localActionStyle = localActionStyle.Underline(true)
			localKeyStyle = localKeyStyle.Underline(true)
			localDescStyle = localDescStyle.Underline(true)
		}

		if i >= m.actionStart && i < m.actionEnd {
			s.WriteString(localActionStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localActionStyle.Render(binding.Help().Desc))
		} else {
			s.WriteString(localKeyStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localDescStyle.Render(binding.Help().Desc))
		}

		// Add appropriate separator
		if i != len(m.options)-1 {
			if slices.Contains(m.groupEnds, i) {
				s.WriteString(sepStyle.Render(verticalSeparator))
			} else {
				s.WriteString(sepStyle.Render(separator))
			}
		}
	}

	centeredMenuText := menuStyle.Render(s.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, centeredMenuText)
}
