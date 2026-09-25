// Package usbep contains the portable, TinyGo-independent logic for
// turning a stream of USB packets (as small as one byte, as large as
// whatever the peripheral's own maximum packet size is) into complete
// USBTMC messages, and for splitting an outgoing message back into
// packets small enough for a real USB IN endpoint to send one at a time.
//
// Deliberately kept free of the "machine"/"machine/usb" packages TinyGo
// provides, which only exist under a TinyGo build and can't be unit
// tested with plain `go test`. The actual hardware glue (in this
// repository's cmd/firmware, gated by the "tinygo" build tag TinyGo sets
// automatically) is expected to be a thin, largely unverifiable-without-
// hardware wrapper around the logic here -- see that package's own doc
// comment for what it does and does not attempt to verify.
package usbep

import "github.com/gotmc/usbtmc/wire"

// Reassembler accumulates USB Bulk-OUT packets into complete USBTMC
// messages (header plus payload), using the message's own TransferSize
// field to know exactly how many bytes belong to it, rather than relying
// on packet-boundary heuristics (a short packet, or a device-specific
// notion of "end of transfer") that a message's own declared length makes
// unnecessary to lean on.
//
// Scope: assumes messages arrive one at a time with no pipelining --
// after a complete message is extracted, any further buffered bytes
// (USBTMC's own alignment padding, 0-3 bytes so the whole transfer is a
// multiple of 4) are discarded rather than treated as the start of the
// next message. This has not been verified against a real host's actual
// framing behavior.
type Reassembler struct {
	buf []byte
}

// Feed appends packet to the buffer and reports a complete message if one
// is now available. The returned message aliases Reassembler's internal
// buffer and is only valid until the next call to Feed; callers that need
// to keep it must copy it first.
func (r *Reassembler) Feed(packet []byte) (message []byte, ready bool) {
	r.buf = append(r.buf, packet...)

	if len(r.buf) < wire.HeaderSize {
		return nil, false
	}

	id, _, ok := wire.DecodeHeaderPrefix(r.buf)
	if !ok {
		// A corrupt prefix can never resolve into a valid message by
		// feeding more bytes; drop it so one bad packet doesn't wedge
		// every future one behind it.
		r.buf = r.buf[:0]
		return nil, false
	}

	want, ok := messageLength(id, r.buf)
	if !ok {
		return nil, false
	}
	if len(r.buf) < want {
		return nil, false
	}

	message = r.buf[:want]
	r.buf = r.buf[:0]
	return message, true
}

// messageLength returns the total length (header plus payload) of the
// message beginning at buf, given its already-decoded MessageID. ok is
// false if buf does not yet carry enough bytes to know that (i.e. fewer
// than a full header), or if id is a message type with no payload to size
// (Trigger and the other zero-payload message types carry only the
// 12-byte header).
func messageLength(id wire.MessageID, buf []byte) (n int, ok bool) {
	if len(buf) < wire.HeaderSize {
		return 0, false
	}
	switch id {
	case wire.DevDepMsgOut:
		_, transferSize, _, ok := wire.DecodeBulkOutHeader(buf)
		if !ok {
			return 0, false
		}
		return wire.HeaderSize + int(transferSize), true
	case wire.RequestDevDepMsgIn:
		_, _, _, _, ok := wire.DecodeRequestDevDepMsgInHeader(buf)
		if !ok {
			return 0, false
		}
		return wire.HeaderSize, true
	default:
		// Trigger and anything else this package does not specifically
		// recognize: no payload beyond the header. usbtmcfront.Handler is
		// responsible for rejecting a MessageID it does not implement:
		// this function's job is only to size the message on the wire,
		// not to validate it.
		return wire.HeaderSize, true
	}
}
