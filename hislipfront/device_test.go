package hislipfront

import (
	"context"
	"errors"
	"testing"

	"github.com/tridentsx/hislip/server"
)

// fakeInstrument is a minimal, fully in-memory gpib.Instrument for testing
// the adapter logic in this package, with no real bus involved.
type fakeInstrument struct {
	remoteEnableCalls []bool
	localLockoutCalls []bool
	goToLocalCalls    int

	statusByte byte
	readErr    error
}

func (f *fakeInstrument) Write(ctx context.Context, p []byte, eoi bool) error { return nil }
func (f *fakeInstrument) Read(ctx context.Context, p []byte) (int, bool, error) {
	return 0, true, f.readErr
}
func (f *fakeInstrument) Clear(ctx context.Context) error   { return nil }
func (f *fakeInstrument) Trigger(ctx context.Context) error { return nil }
func (f *fakeInstrument) ReadStatusByte(ctx context.Context) (byte, error) {
	return f.statusByte, nil
}
func (f *fakeInstrument) RemoteEnable(ctx context.Context, enable bool) error {
	f.remoteEnableCalls = append(f.remoteEnableCalls, enable)
	return nil
}
func (f *fakeInstrument) LocalLockout(ctx context.Context, enable bool) error {
	f.localLockoutCalls = append(f.localLockoutCalls, enable)
	return nil
}
func (f *fakeInstrument) GoToLocal(ctx context.Context) error {
	f.goToLocalCalls++
	return nil
}

// TestRemoteLocalDecomposition checks that RemoteLocal translates each
// IVI-6.1 Table 25 mode into exactly the granular GPIB calls its Effects()
// says it should, and nothing else -- in particular that EffectNoChange
// really does mean "don't call this at all," not "call it with some
// default value."
func TestRemoteLocalDecomposition(t *testing.T) {
	tests := []struct {
		name               string
		mode               server.RemoteLocalMode
		wantRemoteEnable   []bool
		wantLocalLockout   []bool
		wantGoToLocalCalls int
	}{
		{
			name:               "DisableRemote",
			mode:               server.DisableRemote,
			wantRemoteEnable:   []bool{false},
			wantLocalLockout:   []bool{false},
			wantGoToLocalCalls: 0,
		},
		{
			name:               "EnableRemote",
			mode:               server.EnableRemote,
			wantRemoteEnable:   []bool{true},
			wantLocalLockout:   nil,
			wantGoToLocalCalls: 0,
		},
		{
			name:               "EnableRemoteGoToRemote",
			mode:               server.EnableRemoteGoToRemote,
			wantRemoteEnable:   []bool{true},
			wantLocalLockout:   nil,
			wantGoToLocalCalls: 0, // Remote:EffectTrue is a side effect of RemoteEnable(true), not GoToLocal.
		},
		{
			name:               "EnableRemoteLockoutLocal",
			mode:               server.EnableRemoteLockoutLocal,
			wantRemoteEnable:   []bool{true},
			wantLocalLockout:   []bool{true},
			wantGoToLocalCalls: 0,
		},
		{
			name:               "GoToLocal",
			mode:               server.GoToLocal,
			wantRemoteEnable:   nil,
			wantLocalLockout:   nil,
			wantGoToLocalCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeInstrument{}
			d := NewDevice(fake)

			if err := d.RemoteLocal(context.Background(), tt.mode); err != nil {
				t.Fatalf("RemoteLocal: %v", err)
			}

			if !boolSlicesEqual(fake.remoteEnableCalls, tt.wantRemoteEnable) {
				t.Errorf("RemoteEnable calls = %v, want %v", fake.remoteEnableCalls, tt.wantRemoteEnable)
			}
			if !boolSlicesEqual(fake.localLockoutCalls, tt.wantLocalLockout) {
				t.Errorf("LocalLockout calls = %v, want %v", fake.localLockoutCalls, tt.wantLocalLockout)
			}
			if fake.goToLocalCalls != tt.wantGoToLocalCalls {
				t.Errorf("GoToLocal calls = %d, want %d", fake.goToLocalCalls, tt.wantGoToLocalCalls)
			}
		})
	}
}

// TestRemoteLocalInvalidModeIsNoop checks that an undefined mode does
// nothing rather than calling any granular method with a guessed value.
func TestRemoteLocalInvalidModeIsNoop(t *testing.T) {
	fake := &fakeInstrument{}
	d := NewDevice(fake)

	if err := d.RemoteLocal(context.Background(), server.RemoteLocalMode(99)); err != nil {
		t.Fatalf("RemoteLocal: %v", err)
	}
	if len(fake.remoteEnableCalls) != 0 || len(fake.localLockoutCalls) != 0 || fake.goToLocalCalls != 0 {
		t.Errorf("invalid mode made calls: RemoteEnable=%v LocalLockout=%v GoToLocal=%d",
			fake.remoteEnableCalls, fake.localLockoutCalls, fake.goToLocalCalls)
	}
}

// fakeInstrumentWithSRQ additionally implements gpib.ServiceRequestSource.
type fakeInstrumentWithSRQ struct {
	fakeInstrument
	ch chan byte
}

func (f *fakeInstrumentWithSRQ) ServiceRequests() <-chan byte { return f.ch }

// TestOptionalCapabilitiesOnlyPresentWhenBacked is the real point of the
// decorator composition in NewDevice: a server.Device wrapping an
// Instrument that does NOT implement gpib.ServiceRequestSource must not
// satisfy server.ServiceRequestSource either, or the HiSLIP server cannot
// tell "not supported" apart from "supported, nothing happening" -- see
// NewDevice's doc comment.
func TestOptionalCapabilitiesOnlyPresentWhenBacked(t *testing.T) {
	plain := NewDevice(&fakeInstrument{})
	if _, ok := plain.(server.ServiceRequestSource); ok {
		t.Error("plain Instrument's Device satisfies server.ServiceRequestSource, want it not to")
	}
	if _, ok := plain.(server.DeviceResetter); ok {
		t.Error("plain Instrument's Device satisfies server.DeviceResetter, want it not to")
	}
	if _, ok := plain.(server.DeviceInfo); ok {
		t.Error("plain Instrument's Device satisfies server.DeviceInfo, want it not to")
	}

	withSRQ := &fakeInstrumentWithSRQ{ch: make(chan byte, 1)}
	wrapped := NewDevice(withSRQ)
	src, ok := wrapped.(server.ServiceRequestSource)
	if !ok {
		t.Fatal("Instrument with ServiceRequests does not satisfy server.ServiceRequestSource")
	}
	withSRQ.ch <- 0x42
	if got := <-src.ServiceRequests(); got != 0x42 {
		t.Errorf("ServiceRequests() delivered %#x, want 0x42", got)
	}
	// Adding one capability must not remove the base Device methods.
	if err := wrapped.Clear(context.Background()); err != nil {
		t.Errorf("Clear on decorated Device: %v", err)
	}
}

func TestReadStatusByteAndErrorPassthrough(t *testing.T) {
	wantErr := errors.New("bus timeout")
	fake := &fakeInstrument{statusByte: 0x50, readErr: wantErr}
	d := NewDevice(fake)

	if sb, err := d.ReadStatusByte(context.Background()); err != nil || sb != 0x50 {
		t.Errorf("ReadStatusByte() = (%#x, %v), want (0x50, nil)", sb, err)
	}

	_, _, err := d.Read(context.Background(), make([]byte, 8))
	if !errors.Is(err, wantErr) {
		t.Errorf("Read() error = %v, want %v", err, wantErr)
	}
}

func boolSlicesEqual(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
