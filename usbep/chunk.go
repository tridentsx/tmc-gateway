package usbep

// ChunkSender splits one outgoing message into packets no larger than
// maxPacketSize, for a USB IN endpoint that can only hold one packet's
// worth of data in its hardware buffer at a time and signals when that
// buffer is free again (TxHandler, in this repository's real firmware
// glue) rather than accepting an arbitrarily large buffer in one call.
//
// It also emits the trailing zero-length packet USB requires when a
// transfer's real data happens to be an exact multiple of maxPacketSize
// (including when it is empty), so the host does not wait for more data
// that is never coming; a short packet in the ordinary case already
// terminates the transfer without one.
type ChunkSender struct {
	maxPacketSize int
	data          []byte
	offset        int
	sentFinal     bool
}

// NewChunkSender returns a ChunkSender for a USB IN endpoint whose
// hardware buffer holds at most maxPacketSize bytes per packet.
func NewChunkSender(maxPacketSize int) *ChunkSender {
	return &ChunkSender{maxPacketSize: maxPacketSize}
}

// Start begins sending data and returns the first packet to transmit,
// which the caller must send before ever calling Next. Its length may be
// less than maxPacketSize (including zero, for an empty message), which
// on its own already terminates the transfer -- see Next.
func (c *ChunkSender) Start(data []byte) []byte {
	c.data = data
	c.offset = 0
	c.sentFinal = false
	return c.chunk()
}

// Next returns the next packet to transmit, meant to be called exactly
// once per TxHandler completion of the previously sent packet. ok is
// false once the whole transfer -- including a trailing zero-length
// packet if Start's own chunking made one necessary -- has been sent;
// Next must not be called again until Start begins a new transfer.
func (c *ChunkSender) Next() (packet []byte, ok bool) {
	if c.sentFinal {
		return nil, false
	}
	return c.chunk(), true
}

func (c *ChunkSender) chunk() []byte {
	end := min(c.offset+c.maxPacketSize, len(c.data))
	packet := c.data[c.offset:end]
	c.offset = end
	if len(packet) < c.maxPacketSize {
		// A short packet (length 0 included) always terminates a USB
		// bulk transfer; nothing can follow it. This is also what
		// naturally emits the exact-multiple-of-maxPacketSize case's
		// required trailing empty packet: the real data's last chunk is
		// exactly maxPacketSize long (so this branch does not fire for
		// it), and the *next* call computes an empty slice from an
		// already-exhausted offset, which is shorter than maxPacketSize
		// and does fire it.
		c.sentFinal = true
	}
	return packet
}
