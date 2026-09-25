package usbep

import (
	"bytes"
	"testing"

	"github.com/gotmc/usbtmc/wire"
)

func TestReassemblerSingleFeed(t *testing.T) {
	payload := []byte("*IDN?\n")
	hdr := wire.EncodeBulkOutHeader(1, uint32(len(payload)), true)
	msg := append(append([]byte(nil), hdr[:]...), payload...)

	var r Reassembler
	got, ready := r.Feed(msg)
	if !ready {
		t.Fatal("Feed() ready = false, want true after one complete message")
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("Feed() message = %x, want %x", got, msg)
	}
}

// TestReassemblerByteAtATime is the real point of this package: a real
// USB peripheral delivers one small packet per RxHandler call, not one
// complete message. Feeding one byte at a time is the most extreme case
// of that.
func TestReassemblerByteAtATime(t *testing.T) {
	payload := []byte("MEAS:VOLT?\n")
	hdr := wire.EncodeBulkOutHeader(5, uint32(len(payload)), true)
	msg := append(append([]byte(nil), hdr[:]...), payload...)

	var r Reassembler
	for i := 0; i < len(msg)-1; i++ {
		got, ready := r.Feed(msg[i : i+1])
		if ready {
			t.Fatalf("Feed() ready = true after byte %d, want false (message not complete yet); got %x", i, got)
		}
	}
	got, ready := r.Feed(msg[len(msg)-1:])
	if !ready {
		t.Fatal("Feed() ready = false on the final byte, want true")
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("Feed() message = %x, want %x", got, msg)
	}
}

func TestReassemblerRequestDevDepMsgIn(t *testing.T) {
	hdr := wire.EncodeRequestDevDepMsgInHeader(9, 64, false, 0)

	var r Reassembler
	// RequestDevDepMsgIn carries no payload beyond its own 12-byte
	// header, regardless of the TransferSize it asks the device for.
	got, ready := r.Feed(hdr[:])
	if !ready {
		t.Fatal("Feed() ready = false, want true for a bare RequestDevDepMsgIn header")
	}
	if !bytes.Equal(got, hdr[:]) {
		t.Errorf("Feed() message = %x, want %x", got, hdr[:])
	}
}

func TestReassemblerMultipleMessagesSequentially(t *testing.T) {
	payload1 := []byte("A")
	hdr1 := wire.EncodeBulkOutHeader(1, uint32(len(payload1)), true)
	msg1 := append(append([]byte(nil), hdr1[:]...), payload1...)

	payload2 := []byte("BB")
	hdr2 := wire.EncodeBulkOutHeader(2, uint32(len(payload2)), true)
	msg2 := append(append([]byte(nil), hdr2[:]...), payload2...)

	var r Reassembler
	got1, ready1 := r.Feed(msg1)
	if !ready1 || !bytes.Equal(got1, msg1) {
		t.Fatalf("first message: ready=%v got=%x, want ready=true got=%x", ready1, got1, msg1)
	}
	// A copy is required per Feed's own doc comment: got1 aliases the
	// Reassembler's internal buffer, invalidated by the next Feed call.
	got1Copy := append([]byte(nil), got1...)

	got2, ready2 := r.Feed(msg2)
	if !ready2 || !bytes.Equal(got2, msg2) {
		t.Fatalf("second message: ready=%v got=%x, want ready=true got=%x", ready2, got2, msg2)
	}
	if !bytes.Equal(got1Copy, msg1) {
		t.Error("first message's copy was corrupted by feeding the second")
	}
}

func TestReassemblerCorruptPrefixIsDropped(t *testing.T) {
	var r Reassembler
	corrupt := []byte{1, 5, 0x00 /* wrong inverse of 5 */, 0, 0, 0, 0, 0, 1, 0, 0, 0}
	got, ready := r.Feed(corrupt)
	if ready {
		t.Fatalf("Feed() ready = true for a corrupt header, want false; got %x", got)
	}

	// A subsequent real message must not be wedged behind the dropped
	// garbage.
	payload := []byte("ok")
	hdr := wire.EncodeBulkOutHeader(1, uint32(len(payload)), true)
	msg := append(append([]byte(nil), hdr[:]...), payload...)
	got, ready = r.Feed(msg)
	if !ready || !bytes.Equal(got, msg) {
		t.Fatalf("after corrupt prefix: ready=%v got=%x, want ready=true got=%x", ready, got, msg)
	}
}
