package usbtmcfront

import (
	"context"
	"errors"
	"testing"

	"github.com/gotmc/usbtmc/wire"
)

// fakeInstrument is a minimal, fully in-memory gpib.Instrument for testing
// this package's dispatch logic, with no real bus involved.
type fakeInstrument struct {
	writes   [][]byte
	writeEOI []bool
	writeErr error

	readData []byte
	readEOI  bool
	readErr  error

	triggerCalls int
	triggerErr   error
}

func (f *fakeInstrument) Write(ctx context.Context, p []byte, eoi bool) error {
	f.writes = append(f.writes, append([]byte(nil), p...))
	f.writeEOI = append(f.writeEOI, eoi)
	return f.writeErr
}

func (f *fakeInstrument) Read(ctx context.Context, p []byte) (int, bool, error) {
	if f.readErr != nil {
		return 0, false, f.readErr
	}
	n := copy(p, f.readData)
	return n, f.readEOI, nil
}

func (f *fakeInstrument) Clear(ctx context.Context) error   { return nil }
func (f *fakeInstrument) Trigger(ctx context.Context) error { f.triggerCalls++; return f.triggerErr }
func (f *fakeInstrument) ReadStatusByte(ctx context.Context) (byte, error) {
	return 0, nil
}
func (f *fakeInstrument) RemoteEnable(ctx context.Context, enable bool) error { return nil }
func (f *fakeInstrument) GoToLocal(ctx context.Context) error                 { return nil }
func (f *fakeInstrument) LocalLockout(ctx context.Context, enable bool) error { return nil }

func TestHandleDevDepMsgOut(t *testing.T) {
	fake := &fakeInstrument{}
	h := &Handler{Instrument: fake}

	payload := []byte("*IDN?\n")
	msg := append(wireEncodeBulkOut(t, 1, uint32(len(payload)), true), payload...)

	resp, err := h.HandleBulkOut(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleBulkOut: %v", err)
	}
	if resp != nil {
		t.Errorf("HandleBulkOut() response = %v, want nil for DevDepMsgOut", resp)
	}
	if len(fake.writes) != 1 || string(fake.writes[0]) != string(payload) {
		t.Errorf("Instrument.Write got %q, want %q", fake.writes, payload)
	}
	if len(fake.writeEOI) != 1 || !fake.writeEOI[0] {
		t.Errorf("Instrument.Write eoi = %v, want [true]", fake.writeEOI)
	}
}

func TestHandleDevDepMsgOutRejectsTruncatedPayload(t *testing.T) {
	fake := &fakeInstrument{}
	h := &Handler{Instrument: fake}

	// Header claims 100 bytes of payload but the message only carries 3.
	msg := append(wireEncodeBulkOut(t, 1, 100, true), []byte("abc")...)

	if _, err := h.HandleBulkOut(context.Background(), msg); err == nil {
		t.Error("HandleBulkOut() = nil error for a truncated payload, want an error")
	}
	if len(fake.writes) != 0 {
		t.Errorf("Instrument.Write was called on a truncated message, want it not to be")
	}
}

func TestHandleRequestDevDepMsgIn(t *testing.T) {
	fake := &fakeInstrument{readData: []byte("Instrument,1.0\n"), readEOI: true}
	h := &Handler{Instrument: fake}

	msg := wireEncodeRequestIn(t, 7, 64, false, 0)

	resp, err := h.HandleBulkOut(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleBulkOut: %v", err)
	}

	tag, n, eoi, ok := wireDecodeDevDepMsgInHeader(t, resp)
	if !ok {
		t.Fatal("response is not a valid DevDepMsgIn header")
	}
	if tag != 7 {
		t.Errorf("response bTag = %d, want 7 (echoing the request)", tag)
	}
	if !eoi {
		t.Error("response eom = false, want true (Instrument reported eoi)")
	}
	got := resp[wire.HeaderSize : wire.HeaderSize+int(n)]
	if string(got) != string(fake.readData) {
		t.Errorf("response payload = %q, want %q", got, fake.readData)
	}
}

func TestHandleRequestDevDepMsgInCapsAtMaxResponsePayload(t *testing.T) {
	fake := &fakeInstrument{readData: make([]byte, 4096), readEOI: false}
	h := &Handler{Instrument: fake, MaxResponsePayload: 16}

	msg := wireEncodeRequestIn(t, 1, 4096, false, 0)

	resp, err := h.HandleBulkOut(context.Background(), msg)
	if err != nil {
		t.Fatalf("HandleBulkOut: %v", err)
	}
	_, n, _, ok := wireDecodeDevDepMsgInHeader(t, resp)
	if !ok {
		t.Fatal("response is not a valid DevDepMsgIn header")
	}
	if n > 16 {
		t.Errorf("response payload length = %d, want <= 16 (MaxResponsePayload)", n)
	}
}

func TestHandleTrigger(t *testing.T) {
	fake := &fakeInstrument{}
	h := &Handler{Instrument: fake}

	hdr := wire.EncodeHeaderPrefix(3, wire.Trigger)
	resp, err := h.HandleBulkOut(context.Background(), hdr[:])
	if err != nil {
		t.Fatalf("HandleBulkOut: %v", err)
	}
	if resp != nil {
		t.Errorf("HandleBulkOut() response = %v, want nil for Trigger", resp)
	}
	if fake.triggerCalls != 1 {
		t.Errorf("Instrument.Trigger called %d times, want 1", fake.triggerCalls)
	}
}

func TestHandleBulkOutRejectsInvalidHeader(t *testing.T) {
	fake := &fakeInstrument{}
	h := &Handler{Instrument: fake}

	if _, err := h.HandleBulkOut(context.Background(), []byte{1, 2}); err == nil {
		t.Error("HandleBulkOut() = nil error for a too-short header, want an error")
	}
}

func TestHandleDevDepMsgOutPropagatesWriteError(t *testing.T) {
	wantErr := errors.New("bus busy")
	fake := &fakeInstrument{writeErr: wantErr}
	h := &Handler{Instrument: fake}

	msg := append(wireEncodeBulkOut(t, 1, 1, true), 'x')
	_, err := h.HandleBulkOut(context.Background(), msg)
	if !errors.Is(err, wantErr) {
		t.Errorf("HandleBulkOut() error = %v, want %v", err, wantErr)
	}
}

// -- small helpers wrapping the wire package, kept local to this test file
// so the tests above read in terms of USBTMC concepts, not raw byte math.

func wireEncodeBulkOut(t *testing.T, tag byte, transferSize uint32, eom bool) []byte {
	t.Helper()
	hdr := wire.EncodeBulkOutHeader(tag, transferSize, eom)
	return append([]byte(nil), hdr[:]...)
}

func wireEncodeRequestIn(t *testing.T, tag byte, transferSize uint32, termCharEnabled bool, termChar byte) []byte {
	t.Helper()
	hdr := wire.EncodeRequestDevDepMsgInHeader(tag, transferSize, termCharEnabled, termChar)
	return append([]byte(nil), hdr[:]...)
}

func wireDecodeDevDepMsgInHeader(t *testing.T, resp []byte) (tag byte, transferSize uint32, eom bool, ok bool) {
	t.Helper()
	if len(resp) < wire.HeaderSize {
		return 0, 0, false, false
	}
	id, tag, ok := wire.DecodeHeaderPrefix(resp)
	if !ok || id != wire.DevDepMsgIn {
		return 0, 0, false, false
	}
	transferSize = uint32(resp[4]) | uint32(resp[5])<<8 | uint32(resp[6])<<16 | uint32(resp[7])<<24
	eom = resp[8]&0x01 != 0
	return tag, transferSize, eom, true
}
