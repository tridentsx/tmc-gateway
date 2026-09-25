// Command tinygocheck exists only so `just tinygo` has a real main package
// to build: tinygo build, unlike go build, refuses to type-check a bare
// library package and insists on linking a real executable ("expected main
// package to have name main" -- see the identical situation and comment in
// github.com/tridentsx/hislip's own cmd/tinygocheck).
//
// This exercises the real dependency graph of gpib and hislipfront --
// which in turn pulls in hislip/server and hislip/protocol -- for an
// actual embedded target, with a minimal stub Instrument. It does nothing
// real and is not firmware.
package main

import (
	"context"

	"github.com/tridentsx/hislip/server"
	"github.com/tridentsx/tmc-gateway/gpib"
	"github.com/tridentsx/tmc-gateway/hislipfront"
)

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

func main() {
	dev := hislipfront.NewDevice(stubInstrument{})
	if _, err := server.New(dev, server.Config{}); err != nil {
		panic(err)
	}
	for {
	}
}
