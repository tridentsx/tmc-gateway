package usbep

import (
	"bytes"
	"testing"
)

// drain runs a ChunkSender to completion and returns every packet it
// produced, in order, exactly as a real TxHandler-driven loop would
// collect them.
func drain(t *testing.T, maxPacketSize int, data []byte) [][]byte {
	t.Helper()
	c := NewChunkSender(maxPacketSize)
	var packets [][]byte
	packet := c.Start(data)
	packets = append(packets, append([]byte(nil), packet...))
	for {
		packet, ok := c.Next()
		if !ok {
			return packets
		}
		packets = append(packets, append([]byte(nil), packet...))
	}
}

func TestChunkSenderShorterThanOnePacket(t *testing.T) {
	packets := drain(t, 64, []byte("hello"))
	if len(packets) != 1 {
		t.Fatalf("got %d packets, want 1: %v", len(packets), packets)
	}
	if !bytes.Equal(packets[0], []byte("hello")) {
		t.Errorf("packet 0 = %q, want %q", packets[0], "hello")
	}
}

func TestChunkSenderEmptyData(t *testing.T) {
	// An empty message still needs one (zero-length) packet sent, or the
	// host waits forever for data that will never arrive.
	packets := drain(t, 64, nil)
	if len(packets) != 1 {
		t.Fatalf("got %d packets, want 1: %v", len(packets), packets)
	}
	if len(packets[0]) != 0 {
		t.Errorf("packet 0 = %v, want empty", packets[0])
	}
}

func TestChunkSenderExactlyOnePacket(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 64)
	packets := drain(t, 64, data)
	// The real 64-byte packet, plus a trailing zero-length packet since
	// 64 is an exact multiple of maxPacketSize.
	if len(packets) != 2 {
		t.Fatalf("got %d packets, want 2: %v", len(packets), lens(packets))
	}
	if len(packets[0]) != 64 {
		t.Errorf("packet 0 length = %d, want 64", len(packets[0]))
	}
	if len(packets[1]) != 0 {
		t.Errorf("packet 1 (trailing) length = %d, want 0", len(packets[1]))
	}
}

func TestChunkSenderMultiplePackets(t *testing.T) {
	data := bytes.Repeat([]byte("y"), 130) // 64 + 64 + 2
	packets := drain(t, 64, data)
	wantLens := []int{64, 64, 2}
	if len(packets) != len(wantLens) {
		t.Fatalf("got %d packets, want %d: %v", len(packets), len(wantLens), lens(packets))
	}
	for i, want := range wantLens {
		if len(packets[i]) != want {
			t.Errorf("packet %d length = %d, want %d", i, len(packets[i]), want)
		}
	}
	// Reassembling the packets must reproduce the original data exactly.
	var got []byte
	for _, p := range packets {
		got = append(got, p...)
	}
	if !bytes.Equal(got, data) {
		t.Error("concatenated packets do not reproduce the original data")
	}
}

func TestChunkSenderTwoExactPackets(t *testing.T) {
	data := bytes.Repeat([]byte("z"), 128) // exactly 2*64
	packets := drain(t, 64, data)
	wantLens := []int{64, 64, 0} // trailing ZLP since 128 % 64 == 0
	if len(packets) != len(wantLens) {
		t.Fatalf("got %d packets, want %d: %v", len(packets), len(wantLens), lens(packets))
	}
	for i, want := range wantLens {
		if len(packets[i]) != want {
			t.Errorf("packet %d length = %d, want %d", i, len(packets[i]), want)
		}
	}
}

func lens(packets [][]byte) []int {
	out := make([]int, len(packets))
	for i, p := range packets {
		out[i] = len(p)
	}
	return out
}
