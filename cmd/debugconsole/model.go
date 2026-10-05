package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.bug.st/serial"
)

// knownCommand is one fixed menu entry mirroring cmd/firmware/debug.go's
// runCommand switch. Kept as plain data, not hardcoded into Update/View,
// so adding a firmware command later (e.g. once the real GPIB driver
// lands and wires its own diagnostics through the same console -- see
// that file's own doc comment) means adding one line here.
type knownCommand struct {
	name string
	desc string
}

func (c knownCommand) FilterValue() string { return c.name }
func (c knownCommand) Title() string       { return c.name }
func (c knownCommand) Description() string { return c.desc }

func commandItems() []list.Item {
	cmds := []knownCommand{
		{"help", "list commands, as the firmware itself defines them"},
		{"version", "git commit + build time of the running firmware"},
		{"stats", "persistent counters: rx/tx packets, errors, overflows"},
		{"last", "hex dump of the last USBTMC message and response"},
		{"clear", "zero all stats counters"},
		{"uptime", "seconds since boot"},
		{"ping", "liveness check independent of the heartbeat"},
	}
	items := make([]list.Item, len(cmds))
	for i, c := range cmds {
		items[i] = c
	}
	return items
}

// lineMsg is one line of output read from the firmware's CDC console.
type lineMsg string

// sentMsg confirms a command was written to the port.
type sentMsg struct {
	cmd string
	at  time.Time
}

// errMsg carries a port read/write error into the TUI.
type errMsg error

const maxLines = 2000

var (
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	titleStyle = lipgloss.NewStyle().Bold(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

type model struct {
	port     serial.Port
	portName string

	list  list.Model
	vp    viewport.Model
	input textinput.Model

	rawMode bool
	lines   []string
	lastErr error

	width, height int
}

func newModel(port serial.Port, portName string) model {
	delegate := list.NewDefaultDelegate()
	l := list.New(commandItems(), delegate, 0, 0)
	l.Title = "commands"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	ti := textinput.New()
	ti.Placeholder = "raw command"
	ti.Prompt = "> "

	return model{
		port:     port,
		portName: portName,
		list:     l,
		vp:       viewport.New(0, 0),
		input:    ti,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		listWidth := 32
		bodyHeight := m.height - 4 // top panels minus status/input line and margins
		if bodyHeight < 3 {
			bodyHeight = 3
		}
		m.list.SetSize(listWidth-2, bodyHeight-2)
		m.vp.Width = m.width - listWidth - 2
		m.vp.Height = bodyHeight - 2
		m.input.Width = m.width - 4
		return m, nil

	case lineMsg:
		m.lines = append(m.lines, string(msg))
		if len(m.lines) > maxLines {
			m.lines = m.lines[len(m.lines)-maxLines:]
		}
		m.vp.SetContent(joinLines(m.lines))
		m.vp.GotoBottom()
		return m, nil

	case sentMsg:
		m.lines = append(m.lines, "> "+msg.cmd)
		m.vp.SetContent(joinLines(m.lines))
		m.vp.GotoBottom()
		return m, nil

	case errMsg:
		m.lastErr = msg
		return m, nil

	case tea.KeyMsg:
		if m.rawMode {
			return m.updateRawMode(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "/":
			m.rawMode = true
			m.input.Focus()
			return m, nil
		case "enter":
			if item, ok := m.list.SelectedItem().(knownCommand); ok {
				return m, sendCommand(m.port, item.name)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) updateRawMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rawMode = false
		m.input.Blur()
		m.input.Reset()
		return m, nil
	case "enter":
		cmd := m.input.Value()
		m.rawMode = false
		m.input.Blur()
		m.input.Reset()
		if cmd == "" {
			return m, nil
		}
		return m, sendCommand(m.port, cmd)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.width == 0 {
		return "starting..."
	}

	header := titleStyle.Render(fmt.Sprintf(" debugconsole -- %s ", m.portName))

	listPanel := panelStyle.Render(m.list.View())
	vpPanel := panelStyle.Render(m.vp.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, vpPanel)

	var bottom string
	if m.rawMode {
		bottom = m.input.View()
	} else {
		bottom = helpStyle.Render("↑/↓ select · enter send · / raw command · q quit")
	}
	if m.lastErr != nil {
		bottom = errStyle.Render("error: "+m.lastErr.Error()) + "  " + bottom
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, body, bottom)
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
