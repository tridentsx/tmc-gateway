// Command debugconsole is a HOST-side terminal UI for cmd/firmware's CDC
// debug console (see cmd/firmware/debug.go): a menu of the firmware's
// known commands plus a free-text box for anything not in that list yet,
// instead of hand-typing commands into screen/picocom/pyserial every
// time. Shows an explicit port picker (see picker.go) rather than
// silently guessing which device to use -- unless -port is given, in
// which case that port is used directly and the picker is skipped.
// Runs on the development machine (Mac/Linux/Windows) against an
// already-flashed, already-connected board -- it has no "tinygo" build
// tag, imports nothing from machine/*, and is never built for the
// RP2350. Built after a real debugging session where the lack of
// anything but raw typed commands and manual pyserial scripts made
// iterating slower than it needed to be.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.bug.st/serial"
)

func main() {
	portFlag := flag.String("port", "", "serial port (default: show a picker)")
	baud := flag.Int("baud", 115200, "baud rate")
	flag.Parse()

	portName := *portFlag
	if portName == "" {
		chosen, err := pickPort()
		if err != nil {
			fmt.Fprintln(os.Stderr, "debugconsole:", err)
			os.Exit(1)
		}
		portName = chosen
	}

	mode := &serial.Mode{BaudRate: *baud}
	port, err := serial.Open(portName, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "debugconsole: opening %s: %v\n", portName, err)
		os.Exit(1)
	}
	defer port.Close()

	// Explicit, not assumed: a plain open() on some hosts/tools doesn't
	// reliably assert DTR, which TinyGo's CDC Write silently no-ops on
	// (usbLineInfo.lineState <= 0) -- found the hard way earlier this
	// project, debugging why a console appeared to emit exactly one byte
	// and then nothing. go.bug.st/serial's Open does not document
	// asserting these itself, so set them explicitly rather than trust
	// a default.
	_ = port.SetDTR(true)
	_ = port.SetRTS(true)

	m := newModel(port, portName)
	p := tea.NewProgram(m, tea.WithAltScreen())

	go pumpLines(port, p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "debugconsole:", err)
		os.Exit(1)
	}
}

// pumpLines reads newline-delimited output from the firmware's CDC
// console for as long as the port stays open, forwarding each line into
// the running tea.Program. Runs for the lifetime of the process; Read
// unblocks with an error once port is closed at exit, which ends the
// loop.
func pumpLines(port serial.Port, p *tea.Program) {
	scanner := bufio.NewScanner(port)
	for scanner.Scan() {
		p.Send(lineMsg(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		p.Send(errMsg(err))
	}
}

// sendCommand writes one command line to the firmware console. Errors
// are reported back into the TUI rather than returned, since this is
// always called from the tea.Program's own update loop as a tea.Cmd.
func sendCommand(port serial.Port, cmd string) tea.Cmd {
	return func() tea.Msg {
		if _, err := port.Write([]byte(cmd + "\n")); err != nil {
			return errMsg(fmt.Errorf("write %q: %w", cmd, err))
		}
		return sentMsg{cmd: cmd, at: time.Now()}
	}
}
