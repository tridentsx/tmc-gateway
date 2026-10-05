package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"go.bug.st/serial"
)

// portItem is one entry in the port-picker list: a port path, tagged if
// its name looks like a USB CDC ACM device (covers macOS's
// /dev/cu.usbmodem* and Linux's /dev/ttyACM*) -- the same heuristic
// autoDetectPort used before this picker existed, now surfaced instead
// of applied silently.
type portItem struct {
	name   string
	likely bool
}

func (p portItem) FilterValue() string { return p.name }
func (p portItem) Title() string       { return p.name }
func (p portItem) Description() string {
	if p.likely {
		return "likely USB CDC"
	}
	return ""
}

func scanPorts() ([]list.Item, error) {
	names, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	items := make([]list.Item, len(names))
	for i, n := range names {
		items[i] = portItem{name: n, likely: strings.Contains(n, "usbmodem") || strings.Contains(n, "ttyACM")}
	}
	return items, nil
}

type rescanMsg struct {
	items []list.Item
	err   error
}

func rescanTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
		items, err := scanPorts()
		return rescanMsg{items: items, err: err}
	})
}

// pickerModel is a standalone screen shown before the main console
// connects to anything: pick a port rather than silently trusting
// whatever autoDetectPort would have guessed. Rescans once a second so a
// board plugged in after this screen opens shows up without needing an
// explicit refresh keystroke.
type pickerModel struct {
	list     list.Model
	chosen   string
	canceled bool
	err      error
}

func newPickerModel() pickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "select a serial port (r to rescan, enter to connect, q to quit)"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return pickerModel{list: l}
}

func (m pickerModel) Init() tea.Cmd {
	items, err := scanPorts()
	return tea.Batch(func() tea.Msg { return rescanMsg{items: items, err: err} }, rescanTick())
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width-2, msg.Height-2)
		return m, nil

	case rescanMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, rescanTick()
		}
		m.err = nil
		m.list.SetItems(msg.items)
		return m, rescanTick()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "r":
			items, err := scanPorts()
			return m, func() tea.Msg { return rescanMsg{items: items, err: err} }
		case "enter":
			if item, ok := m.list.SelectedItem().(portItem); ok {
				m.chosen = item.name
				return m, tea.Quit
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	view := m.list.View()
	if m.err != nil {
		view += fmt.Sprintf("\n(scan error: %v)", m.err)
	}
	if len(m.list.Items()) == 0 {
		view += "\nno serial ports found -- waiting for one to appear..."
	}
	return view
}

// pickPort runs the picker as its own tea.Program and returns the chosen
// port name, or an error if the user canceled or nothing was picked.
func pickPort() (string, error) {
	p := tea.NewProgram(newPickerModel())
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}
	pm := finalModel.(pickerModel)
	if pm.canceled || pm.chosen == "" {
		return "", fmt.Errorf("no port selected")
	}
	return pm.chosen, nil
}
