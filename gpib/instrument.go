// Package gpib defines the abstraction every host-facing protocol front end
// on this board -- HiSLIP (over PoE/Ethernet), USBTMC, and USBTMC-USB488
// (both over USB) -- talks to in order to reach the real GPIB instrument on
// the bus. A front end must not know that the bus is GPIB rather than some
// other instrument bus, or a simulation; everything protocol-specific stays
// in that front end's own adapter, and everything bus-specific stays behind
// Instrument.
//
// This mirrors the design already used by
// github.com/tridentsx/hislip/server's own Device interface -- that
// project's doc comment states the identical principle ("The server must
// not know that the device is GPIB, or USB, or a simulation") -- but is not
// simply that interface renamed. Instrument.RemoteEnable/GoToLocal/
// LocalLockout are three separate methods rather than hislip's single
// RemoteLocal(mode) call, because USB488's own control requests
// (REN_CONTROL, GO_TO_LOCAL, LOCAL_LOCKOUT -- USBTMC-USB488 Table 9) are
// three separate requests, not one combined mode code the way HiSLIP's
// AsyncRemoteLocalControl message is (IVI-6.1 Table 25). Decomposing to the
// granular GPIB primitives lets both front ends translate onto the same
// abstraction directly instead of one of them needing to first reconstruct
// a combined mode it never actually received. hislip's own
// RemoteLocalMode.Effects() already computes which of these three to call
// and with what value for a HiSLIP AsyncRemoteLocalControl message; a
// hislip/server.Device adapter over an Instrument is expected to call
// through to it using that.
package gpib

import "context"

// Instrument is the real GPIB instrument behind a protocol front end.
type Instrument interface {
	// Write delivers bytes of a program message to the instrument. eoi
	// reports whether the final byte of p should be sent with the GPIB EOI
	// line asserted, meaning this is the end of the message. A write with
	// eoi true and no bytes still terminates the logical message.
	Write(ctx context.Context, p []byte, eoi bool) error

	// Read fills p with response bytes from the instrument. eoi reports
	// that the response is complete, because EOI was seen on the final
	// byte or a configured EOS terminator was accepted; n may be less than
	// len(p) either way.
	Read(ctx context.Context, p []byte) (n int, eoi bool, err error)

	// Clear performs a GPIB Selected Device Clear against the instrument.
	// This is not the bus-wide universal Device Clear; see InterfaceClear
	// for that.
	Clear(ctx context.Context) error

	// Trigger performs a GPIB Group Execute Trigger against the
	// instrument.
	Trigger(ctx context.Context) error

	// ReadStatusByte performs a GPIB serial poll and returns the
	// instrument's status byte.
	ReadStatusByte(ctx context.Context) (byte, error)

	// RemoteEnable asserts or deasserts the GPIB REN line.
	RemoteEnable(ctx context.Context, enable bool) error

	// GoToLocal addresses the instrument and sends GPIB Go To Local,
	// without changing the state of REN.
	GoToLocal(ctx context.Context) error

	// LocalLockout asserts or deasserts GPIB Local Lockout, which disables
	// (true) or re-enables (false) the instrument's own front-panel
	// controls while REN is asserted.
	LocalLockout(ctx context.Context, enable bool) error
}

// ServiceRequestSource is an optional Instrument capability for backends
// that can report GPIB SRQ asynchronously (typically via a bus interrupt
// rather than only discoverable by polling ReadStatusByte). The byte
// delivered is the status byte at the time SRQ was asserted.
//
// A front end must not send a further event for the same condition until
// the host has read the status byte that reported it; whether that means
// buffering exactly one event or dropping later ones while one is
// outstanding is the implementation's choice, but the host must never see
// the same SRQ reported twice.
type ServiceRequestSource interface {
	ServiceRequests() <-chan byte
}

// InterfaceClear is an optional Instrument capability for backends that can
// issue a bus-wide GPIB Interface Clear (IFC), distinct from the
// per-instrument Clear (Selected Device Clear).
type InterfaceClear interface {
	InterfaceClear(ctx context.Context) error
}

// Name is an optional Instrument capability supplying a human-readable name
// for diagnostics and for the mDNS TXT records a LAN-facing front end (e.g.
// HiSLIP) may advertise.
type Name interface {
	Name() string
}
