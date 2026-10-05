//go:build tinygo

// Command firmware is the real RP2354A firmware entry point -- and, as of
// this file, still mostly a skeleton. It wires a real USB descriptor
// (CDC debug console + USBTMC/USB488, see usbdescriptor.go) through to
// usbtmcfront.Handler via usbep's portable reassembly/chunking, backed by
// a stub gpib.Instrument (the real GPIB bus driver does not exist yet).
//
// Confirmed on real hardware (a W5500-EVB-Pico2, RP2350): the composed USB
// descriptor enumerates exactly as designed (3 interfaces, correct
// classes, correct endpoint addresses). usbtmcRxHandler/usbtmcTxHandler
// run in USB interrupt context, so they never call println (or anything
// else that can block on the CDC ring/TX-complete IRQ) directly -- an
// earlier version did, and froze the device the first time a bulk
// transfer arrived, found on real hardware. They only record events into
// a tiny single-producer/single-consumer ring (logEvent); the main loop,
// running in ordinary (non-interrupt) context, drains and prints them,
// and also prints a heartbeat so a frozen device is distinguishable from
// one that is merely idle. See debug.go for the interactive CDC console
// (type "help" into the serial port) and the persistent counters it
// reports -- built so a future real-hardware debugging session has more
// to go on than this one started with.
package main

import (
	"context"
	"time"

	"machine"
	"machine/usb"

	"github.com/tridentsx/tmc-gateway/gpib"
	"github.com/tridentsx/tmc-gateway/usbep"
	"github.com/tridentsx/tmc-gateway/usbtmcfront"
)

// usbtmcHardwareEndpoint is the raw hardware endpoint number USBTMC's
// bulk IN and OUT pipes share, in the numbering usb.EndpointConfig.Index
// and machine.SendUSBInPacket actually use (confirmed against
// EnableCDC's own usage, e.g. usb.CDC_ENDPOINT_OUT == 2 for hardware EP2)
// -- deliberately not usbdescriptor.go's usbtmcEndpoint
// (descriptor.EndpointEP3 == 2), which is a different, 0-based enum used
// only for building descriptor bytes. Both happen to name "EP3", but as
// different numbers (3 here, 2 there); conflating them would silently
// wire the wrong hardware endpoint.
const usbtmcHardwareEndpoint = 3

// stubInstrument stands in for the real GPIB bus driver, which does not
// exist yet. It does nothing real; see gpib.Instrument's own doc comment
// for what a real implementation must do.
type stubInstrument struct{}

func (stubInstrument) Write(ctx context.Context, p []byte, eoi bool) error { return nil }
func (stubInstrument) Read(ctx context.Context, p []byte) (int, bool, error) {
	return 0, true, nil
}
func (stubInstrument) Clear(ctx context.Context) error                     { return nil }
func (stubInstrument) Trigger(ctx context.Context) error                   { return nil }
func (stubInstrument) ReadStatusByte(ctx context.Context) (byte, error)    { return 0, nil }
func (stubInstrument) RemoteEnable(ctx context.Context, enable bool) error { return nil }
func (stubInstrument) GoToLocal(ctx context.Context) error                 { return nil }
func (stubInstrument) LocalLockout(ctx context.Context, enable bool) error { return nil }

var _ gpib.Instrument = stubInstrument{}

var (
	handler   = &usbtmcfront.Handler{Instrument: stubInstrument{}}
	reasm     usbep.Reassembler
	sender    = usbep.NewChunkSender(usb.EndpointPacketSize)
	txPending bool
)

// rxPacket is one raw USB packet, captured with a plain fixed-size copy
// (no allocation) so usbtmcRxHandler can be interrupt-safe. 64 bytes
// covers full-speed USBTMC's own max packet size.
type rxPacket struct {
	len int
	buf [64]byte
}

// rxRing is a fixed-size single-producer (usbtmcRxHandler, USB interrupt
// context)/single-consumer (main's loop) ring, same shape and reasoning
// as events. Reassembly, dispatch, and response encoding -- all of which
// can allocate (reasm.Feed's returned slice, append, HandleBulkOut's
// response buffer) -- happen only in the main loop, never in the
// handler: TinyGo's bare-metal allocator is not interrupt-safe, so
// anything that can allocate while running as a true hardware interrupt
// (preempting the main thread mid-allocation) can corrupt the heap. An
// earlier version called reasm.Feed/append/HandleBulkOut directly from
// usbtmcRxHandler and froze/reset the device the moment a real bulk
// transfer arrived, on real hardware -- this is the fix for that.
const rxRingSize = 8

var (
	rxRing [rxRingSize]rxPacket
	rxHead uint32
	rxTail uint32
)

// event codes for logEvent/the main loop's drain-and-print switch.
const (
	evRx = iota
	evNotReady
	evReady
	evHandleErr
	evNilResp
	evSending
	evSendStart
	evTxFired
	evTxDone
)

type event struct {
	code byte
	val  int
}

// events is a fixed-size single-producer (RxHandler/TxHandler, USB
// interrupt context)/single-consumer (main's loop, ordinary context)
// ring. logEvent never blocks and never calls anything that can: it is
// the only thing an interrupt-context handler in this file is allowed to
// do, after the earlier println-in-ISR freeze found on real hardware.
var (
	events [32]event
	evHead uint32
	evTail uint32
)

func logEvent(code byte, val int) {
	if evHead-evTail >= uint32(len(events)) {
		stats.evOverflows++
	}
	events[evHead%uint32(len(events))] = event{code, val}
	evHead++
}

func main() {
	// Deliberately not calling machine.EnableCDC here: initUSB (TinyGo's
	// own runtime, before main runs at all) already called it via
	// machine/usb/cdc.EnableUSBCDC, with that package's own real,
	// ring-buffer-backed handlers -- confirmed by reading its source.
	// Calling EnableCDC again here was this file's first real bug, found
	// on real hardware: it overwrote those working handlers (and CDC's
	// own control-request handler, SetupConfig index 0) with this file's
	// no-op stubs, silently breaking the console and println's output
	// along with it. Only USBTMC's own descriptor and endpoints need
	// registering here.
	println("firmware: boot, version =", firmwareVersion)

	machine.ConfigureUSBEndpoint(
		usbtmcDescriptor,
		[]usb.EndpointConfig{
			{
				Index:     usbtmcHardwareEndpoint,
				IsIn:      false,
				Type:      usb.ENDPOINT_TYPE_BULK,
				RxHandler: usbtmcRxHandler,
			},
			{
				Index:     usbtmcHardwareEndpoint,
				IsIn:      true,
				Type:      usb.ENDPOINT_TYPE_BULK,
				TxHandler: usbtmcTxHandler,
			},
		},
		nil, // no control-transfer handler for interface 2 yet -- see usbtmcfront's stated scope.
	)

	// Polled every tick (100ms) rather than once a second: the debug
	// console (pollDebugConsole) should feel responsive, not lag behind
	// a 1s heartbeat cadence. The heartbeat itself still only prints
	// once every heartbeatTicks ticks.
	const heartbeatTicks = 10
	var tick uint32
	for {
		for rxTail != rxHead {
			idx := rxTail % rxRingSize
			rxTail++
			processPacket(rxRing[idx].buf[:rxRing[idx].len])
		}
		for evTail != evHead {
			e := events[evTail%uint32(len(events))]
			evTail++
			printEvent(e)
		}
		pollDebugConsole()
		tick++
		if tick%heartbeatTicks == 0 {
			uptimeSeconds++
			println("heartbeat", uptimeSeconds)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// processPacket runs the real reassembly/dispatch/response-encoding path
// for one raw USB packet, copied out of rxRing by usbtmcRxHandler. Always
// called from the main loop, never from interrupt context -- see rxRing's
// own doc comment for why that matters.
func processPacket(packet []byte) {
	msg, ready := reasm.Feed(packet)
	if !ready {
		stats.rxNotReady++
		logEvent(evNotReady, 0)
		return
	}
	stats.msgsReady++
	lastMsgLen = copy(lastMsg[:], msg)
	logEvent(evReady, len(msg))
	owned := append([]byte(nil), msg...)

	resp, err := handler.HandleBulkOut(context.Background(), owned)
	if err != nil {
		stats.handleErrors++
		logEvent(evHandleErr, 0)
		// No error-reporting path back to the host exists yet (that
		// needs USBTMC's abort/status control requests, which
		// usbtmcfront does not implement -- see its doc comment). The
		// request is simply dropped.
		return
	}
	if resp == nil {
		stats.nilResponses++
		logEvent(evNilResp, 0)
		return // DevDepMsgOut, Trigger: no response message.
	}
	stats.responsesSent++
	lastRespLen = copy(lastResp[:], resp)
	logEvent(evSending, len(resp))
	startSend(resp)
}

func printEvent(e event) {
	switch e.code {
	case evRx:
		println("rx: got packet len", e.val)
	case evNotReady:
		println("rx: not ready yet")
	case evReady:
		println("rx: message ready, len", e.val)
	case evHandleErr:
		println("rx: HandleBulkOut error")
	case evNilResp:
		println("rx: nil response (DevDepMsgOut/Trigger)")
	case evSending:
		println("rx: sending response, len", e.val)
	case evSendStart:
		println("tx: SendUSBInPacket ok =", e.val != 0)
	case evTxFired:
		println("tx: handler fired")
	case evTxDone:
		println("tx: done")
	}
}

// usbtmcRxHandler is called once per received USB packet on USBTMC's
// bulk-OUT endpoint (a real peripheral packet, not a reassembled
// transfer -- see usbep.Reassembler's own doc comment for why that
// matters), in USB interrupt context. It only copies the packet into
// rxRing (a plain fixed-size array copy, no allocation) for the main
// loop to actually reassemble and dispatch -- see rxRing's own doc
// comment for why nothing else is safe to do here.
func usbtmcRxHandler(packet []byte) {
	stats.rxPackets++
	if rxHead-rxTail >= rxRingSize {
		stats.rxOverflows++
	}
	logEvent(evRx, len(packet))
	idx := rxHead % rxRingSize
	rxRing[idx].len = copy(rxRing[idx].buf[:], packet)
	rxHead++
}

// usbtmcTxHandler is called once the previously sent USB packet on
// USBTMC's bulk-IN endpoint has actually gone out and the hardware buffer
// is free again -- the only correct place to send the next chunk of a
// message longer than one packet; see usbep.ChunkSender's own doc
// comment. Also USB interrupt context; see usbtmcRxHandler's doc comment.
func usbtmcTxHandler() {
	stats.txFired++
	logEvent(evTxFired, 0)
	if !txPending {
		return
	}
	packet, ok := sender.Next()
	if !ok {
		txPending = false
		stats.txDone++
		logEvent(evTxDone, 0)
		return
	}
	sendPacket(packet)
}

func startSend(data []byte) {
	txPending = true
	ok := sendPacket(sender.Start(data))
	logEvent(evSendStart, boolToInt(ok))
}

// sendPacket is the one place that actually calls machine.SendUSBInPacket
// for USBTMC's bulk-IN endpoint, so txPacketsSent/txSendFailures account
// for every send regardless of whether it came from startSend (the first
// packet of a response) or usbtmcTxHandler (every packet after that).
func sendPacket(packet []byte) bool {
	ok := machine.SendUSBInPacket(usbtmcHardwareEndpoint, packet)
	stats.txPacketsSent++
	if !ok {
		stats.txSendFailures++
	}
	return ok
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
