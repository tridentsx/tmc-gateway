//go:build tinygo

package main

import "machine"

// firmwareStats are plain counters: some incremented from USB interrupt
// context (usbtmcRxHandler, usbtmcTxHandler, sendPacket), some from the
// main loop (processPacket). Only ever read/printed from the main loop.
// Single RP2350 core, no concurrent main-loop code, so plain increments
// are safe without atomics -- same reasoning as rxRing/events elsewhere
// in this package. Exists so a real prototype session can run "stats"
// over the CDC console instead of needing another ad hoc debugging pass
// like the one that found the EnableCDC and ISR-allocation bugs.
type firmwareStats struct {
	rxPackets      uint32 // usbtmcRxHandler: every packet, before reassembly
	rxOverflows    uint32 // rxRing was full; oldest unread packet was dropped
	evOverflows    uint32 // events ring was full; oldest unread event was dropped
	rxNotReady     uint32 // processPacket: reasm.Feed wanted more packets
	msgsReady      uint32 // processPacket: a complete USBTMC message arrived
	handleErrors   uint32 // processPacket: usbtmcfront.Handler.HandleBulkOut errored
	nilResponses   uint32 // processPacket: DevDepMsgOut/Trigger, no response expected
	responsesSent  uint32 // processPacket: a DevDepMsgIn response was handed to startSend
	txFired        uint32 // usbtmcTxHandler: TX-complete IRQ fired
	txDone         uint32 // usbtmcTxHandler: chunked send finished (sender.Next() exhausted)
	txPacketsSent  uint32 // sendPacket: every machine.SendUSBInPacket call
	txSendFailures uint32 // sendPacket: machine.SendUSBInPacket returned false
}

var stats firmwareStats

// uptimeSeconds is incremented once per heartbeat (see main's loop), and
// read by the "uptime" debug command.
var uptimeSeconds uint32

// lastMsg/lastResp hold a bounded copy of the most recently reassembled
// USBTMC message and the response sent back for it, for the "last" debug
// command. Fixed size, no allocation; truncates anything longer than the
// buffer (fine for a diagnostic dump, not a correctness path).
var (
	lastMsg     [64]byte
	lastMsgLen  int
	lastResp    [64]byte
	lastRespLen int
)

const cmdBufSize = 64

var (
	cmdBuf [cmdBufSize]byte
	cmdLen int
)

// pollDebugConsole reads any bytes typed into the CDC console and
// dispatches a command once a full line arrives. Only ever called from
// the main loop -- machine.USBCDC.ReadByte, like machine.USBCDC.Write,
// is not something usbtmcRxHandler/usbtmcTxHandler may call directly;
// see those functions' own doc comments for why.
func pollDebugConsole() {
	for machine.USBCDC.Buffered() > 0 {
		b, err := machine.USBCDC.ReadByte()
		if err != nil {
			return
		}
		switch {
		case b == '\r' || b == '\n':
			if cmdLen > 0 {
				runCommand(cmdBuf[:cmdLen])
				cmdLen = 0
			}
		case cmdLen < len(cmdBuf):
			cmdBuf[cmdLen] = b
			cmdLen++
		default:
			// Line too long for cmdBuf: drop the byte rather than
			// overflow, and let the eventual \n flush whatever fit.
		}
	}
}

func runCommand(cmd []byte) {
	switch string(cmd) {
	case "help":
		println("commands: help stats last clear uptime ping")
	case "stats":
		printStats()
	case "last":
		printLast()
	case "clear":
		stats = firmwareStats{}
		println("stats cleared")
	case "uptime":
		println("uptime_s =", uptimeSeconds)
	case "ping":
		println("pong")
	default:
		println("unknown command (try: help)")
	}
}

func printStats() {
	println("-- stats --")
	println("uptime_s        =", uptimeSeconds)
	println("rx_packets      =", stats.rxPackets)
	println("rx_overflows    =", stats.rxOverflows)
	println("event_overflows =", stats.evOverflows)
	println("rx_not_ready    =", stats.rxNotReady)
	println("msgs_ready      =", stats.msgsReady)
	println("handle_errors   =", stats.handleErrors)
	println("nil_responses   =", stats.nilResponses)
	println("responses_sent  =", stats.responsesSent)
	println("tx_fired        =", stats.txFired)
	println("tx_done         =", stats.txDone)
	println("tx_packets_sent =", stats.txPacketsSent)
	println("tx_send_fails   =", stats.txSendFailures)
}

func printLast() {
	println("last_msg_len =", lastMsgLen)
	printHex(lastMsg[:lastMsgLen])
	println("last_resp_len =", lastRespLen)
	printHex(lastResp[:lastRespLen])
}

const hexDigits = "0123456789abcdef"

// printHex prints b as space-separated hex bytes on one line. Hand-rolled
// rather than fmt: nothing else in this firmware pulls in fmt, and a
// one-line hex dump doesn't need it.
func printHex(b []byte) {
	if len(b) == 0 {
		println("(empty)")
		return
	}
	var buf [3 * len(lastMsg)]byte
	n := 0
	for _, c := range b {
		buf[n] = hexDigits[c>>4]
		buf[n+1] = hexDigits[c&0xf]
		buf[n+2] = ' '
		n += 3
	}
	println(string(buf[:n-1]))
}
