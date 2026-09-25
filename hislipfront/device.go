// Package hislipfront adapts a gpib.Instrument to the
// github.com/tridentsx/hislip/server.Device interface, so a HiSLIP server
// on this board's Ethernet/PoE side can drive the same GPIB instrument the
// USB-side USBTMC/USB488 front end drives.
//
// This is the only place hislip's own vocabulary (RemoteLocalMode, its
// Table-25 Effects) meets gpib's granular primitives (RemoteEnable,
// LocalLockout, GoToLocal); see gpib's package doc comment for why they are
// not the same shape.
package hislipfront

import (
	"context"

	"github.com/tridentsx/hislip/server"
	"github.com/tridentsx/tmc-gateway/gpib"
)

// device implements the required part of server.Device over a
// gpib.Instrument. NewDevice, not this type, is what callers use: it
// decides which of the optional server.Device capabilities to add based on
// what instr actually supports.
type device struct {
	instrument gpib.Instrument
}

var _ server.Device = (*device)(nil)

// Write implements server.Device.
func (d *device) Write(ctx context.Context, p []byte, end bool) error {
	return d.instrument.Write(ctx, p, end)
}

// Read implements server.Device.
func (d *device) Read(ctx context.Context, p []byte) (n int, end bool, err error) {
	return d.instrument.Read(ctx, p)
}

// Clear implements server.Device.
func (d *device) Clear(ctx context.Context) error {
	return d.instrument.Clear(ctx)
}

// Trigger implements server.Device.
func (d *device) Trigger(ctx context.Context) error {
	return d.instrument.Trigger(ctx)
}

// ReadStatusByte implements server.Device.
func (d *device) ReadStatusByte(ctx context.Context) (byte, error) {
	return d.instrument.ReadStatusByte(ctx)
}

// RemoteLocal implements server.Device by decomposing mode's IVI-6.1
// Table 25 effects into the granular GPIB calls gpib.Instrument exposes.
//
// mode.Effects().Remote reflects whether the instrument ends up addressed
// into remote mode, but IEEE-488.1 has no bus operation for "enter remote"
// distinct from being addressed to listen while REN is asserted -- it is a
// side effect of RemoteEnable(true), not a separate action, so Remote's
// EffectTrue never needs an extra call here (Table 25 only ever pairs it
// with RemoteEnable's own EffectTrue in the same mode).
//
// Remote's EffectFalse is subtler. DisableRemote and DisableRemoteGoToLocal
// both have Effects {False, False, False} -- RemoteEnable false as well --
// and deasserting REN alone reverts every instrument on the bus to local
// per IEEE-488.1 §2.4, with no addressed command needed. The GoToLocal mode
// alone has Effects {NoChange, NoChange, False}: REN stays asserted, and
// only this one instrument needs to leave remote, which does need the
// explicit addressed Go To Local command. So GoToLocal is called only when
// Remote is EffectFalse *and* RemoteEnable is EffectNoChange, not whenever
// Remote is EffectFalse. Caught by TestRemoteLocalDecomposition's
// DisableRemote case, which failed with the broader condition. This has
// not been verified against real GPIB hardware yet.
func (d *device) RemoteLocal(ctx context.Context, mode server.RemoteLocalMode) error {
	if !mode.Valid() {
		return nil
	}
	effects := mode.Effects()

	if effects.RemoteEnable != server.EffectNoChange {
		if err := d.instrument.RemoteEnable(ctx, effects.RemoteEnable == server.EffectTrue); err != nil {
			return err
		}
	}
	if effects.LocalLockout != server.EffectNoChange {
		if err := d.instrument.LocalLockout(ctx, effects.LocalLockout == server.EffectTrue); err != nil {
			return err
		}
	}
	if effects.Remote == server.EffectFalse && effects.RemoteEnable == server.EffectNoChange {
		if err := d.instrument.GoToLocal(ctx); err != nil {
			return err
		}
	}
	return nil
}

// NewDevice adapts instr to server.Device. The returned value implements
// server.ServiceRequestSource, server.DeviceResetter, or
// server.DeviceInfo only if instr itself implements the corresponding
// gpib capability (ServiceRequestSource, InterfaceClear, or Name) --
// deliberately not unconditionally, since the HiSLIP server uses a type
// assertion against the returned value to tell "not supported" apart from
// "supported, and nothing is happening," and getting that wrong in either
// direction is a real protocol-correctness bug, not just a missing
// convenience.
//
// Each optional capability, when present, wraps the previous value rather
// than replacing it, so the whole set composes: adding
// server.DeviceResetter does not undo server.ServiceRequestSource added a
// moment before. Embedding server.Device (an interface value, not a
// struct) is what makes this promotion work.
func NewDevice(instr gpib.Instrument) server.Device {
	var d server.Device = &device{instrument: instr}

	if src, ok := instr.(gpib.ServiceRequestSource); ok {
		d = serviceRequestDevice{Device: d, src: src}
	}
	if ic, ok := instr.(gpib.InterfaceClear); ok {
		d = interfaceClearDevice{Device: d, ic: ic}
	}
	if n, ok := instr.(gpib.Name); ok {
		d = namedDevice{Device: d, name: n}
	}
	return d
}

// serviceRequestDevice adds server.ServiceRequestSource.
type serviceRequestDevice struct {
	server.Device
	src gpib.ServiceRequestSource
}

func (d serviceRequestDevice) ServiceRequests() <-chan byte {
	return d.src.ServiceRequests()
}

// interfaceClearDevice adds server.DeviceResetter.
type interfaceClearDevice struct {
	server.Device
	ic gpib.InterfaceClear
}

func (d interfaceClearDevice) InterfaceClear(ctx context.Context) error {
	return d.ic.InterfaceClear(ctx)
}

// namedDevice adds server.DeviceInfo.
type namedDevice struct {
	server.Device
	name gpib.Name
}

func (d namedDevice) DeviceName() string {
	return d.name.Name()
}
