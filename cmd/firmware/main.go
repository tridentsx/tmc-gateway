//go:build tinygo

// Command firmware is the real RP2354A firmware entry point -- and, as of
// this file, still mostly a skeleton. It wires a real USB descriptor
// (CDC debug console + USBTMC/USB488, see usbdescriptor.go) through to
// usbtmcfront.Handler via usbep's portable reassembly/chunking, backed by
// a stub gpib.Instrument (the real GPIB bus driver does not exist yet).
//
// Confirmed to build for real with tinygo build -target=pico2 (0.42.0),
// which is the only verification this file has had: no RP2354A board has
// run any of this, and the USB peripheral wiring in particular (endpoint
// numbering, the CDC+USBTMC descriptor composition, the TxHandler-driven
// chunked send) has not been checked against real enumeration behavior
// on a real USB host. Treat this as a real, structurally-argued attempt,
// not a verified one.
package main

import (
	"context"

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
func (stubInstrument) Trigger(ctx context.Context) error                  { return nil }
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

func main() {
	machine.EnableCDC(cdcTxHandler, cdcRxHandler, cdcSetupHandler)

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

	for {
	}
}

// cdcTxHandler, cdcRxHandler, and cdcSetupHandler back the debug console.
// Nothing real yet: no line discipline, no echo, no REPL.
func cdcTxHandler()             {}
func cdcRxHandler(data []byte)  {}
func cdcSetupHandler(s usb.Setup) bool { return false }

// usbtmcRxHandler is called once per received USB packet on USBTMC's
// bulk-OUT endpoint (a real peripheral packet, not a reassembled
// transfer -- see usbep.Reassembler's own doc comment for why that
// matters). Feeds it to the reassembler, and once a complete message is
// available, copies it out (Feed's own contract: the returned slice is
// only valid until the next Feed call) and dispatches it.
func usbtmcRxHandler(packet []byte) {
	msg, ready := reasm.Feed(packet)
	if !ready {
		return
	}
	owned := append([]byte(nil), msg...)

	resp, err := handler.HandleBulkOut(context.Background(), owned)
	if err != nil {
		// No error-reporting path back to the host exists yet (that
		// needs USBTMC's abort/status control requests, which
		// usbtmcfront does not implement -- see its doc comment). The
		// request is simply dropped.
		return
	}
	if resp == nil {
		return // DevDepMsgOut, Trigger: no response message.
	}
	startSend(resp)
}

// usbtmcTxHandler is called once the previously sent USB packet on
// USBTMC's bulk-IN endpoint has actually gone out and the hardware buffer
// is free again -- the only correct place to send the next chunk of a
// message longer than one packet; see usbep.ChunkSender's own doc
// comment.
func usbtmcTxHandler() {
	if !txPending {
		return
	}
	packet, ok := sender.Next()
	if !ok {
		txPending = false
		return
	}
	machine.SendUSBInPacket(usbtmcHardwareEndpoint, packet)
}

func startSend(data []byte) {
	txPending = true
	machine.SendUSBInPacket(usbtmcHardwareEndpoint, sender.Start(data))
}
