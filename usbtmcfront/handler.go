// Package usbtmcfront adapts USBTMC/USB488 Bulk-OUT/Bulk-IN messages to a
// gpib.Instrument, the same abstraction github.com/tridentsx/tmc-gateway's
// hislipfront package adapts HiSLIP to. See gpib's own package doc comment
// for why the abstraction is shaped the way it is; this package is USBTMC's
// side of the same story usbtmcfront/hislipfront both tell.
//
// Handler works on one complete, already-reassembled logical USBTMC
// message at a time -- decoding the wire bytes of a real USB transfer
// (splitting a message across several USB packets, and the alignment
// padding USBTMC requires between messages, e.g. per USBTMC §3.2) is not
// this package's job. That boundary matches
// github.com/tridentsx/hislip/server's own Stream abstraction, which
// separates transport framing from protocol logic the same way.
//
// Scope, deliberately not yet handled: TermChar-based early read
// termination (the termCharEnabled/termChar fields of a
// RequestDevDepMsgIn) is decoded but ignored -- Handler always reads up to
// MaxResponsePayload or until gpib.Instrument.Read reports eoi, the same
// as if TermChar were never requested. HiSLIP's server package has an
// analogous optional capability (ReadRequester) for exactly this kind of
// early termination; a usbtmcfront equivalent needing an analogous
// optional gpib capability is future work, not implemented here. Control-
// transfer-based USBTMC/USB488 requests (GET_CAPABILITIES, INITIATE_CLEAR,
// READ_STATUS_BYTE, REN_CONTROL, GO_TO_LOCAL, LOCAL_LOCKOUT, and the abort
// requests) are not implemented at all yet; only the three Bulk-OUT
// message types are.
package usbtmcfront

import (
	"context"
	"fmt"

	"github.com/gotmc/usbtmc/wire"
	"github.com/tridentsx/tmc-gateway/gpib"
)

// defaultMaxResponsePayload bounds how much of a RequestDevDepMsgIn's
// requested TransferSize Handler will actually try to read at once, since
// an embedded target cannot allocate an arbitrarily large buffer just
// because a host asked for one. It has no relationship to any USBTMC
// spec value; it is only this package's own default.
const defaultMaxResponsePayload = 4096

// Handler adapts USBTMC/USB488 Bulk-OUT messages to a gpib.Instrument.
type Handler struct {
	// Instrument is the real GPIB instrument this handler drives.
	Instrument gpib.Instrument

	// MaxResponsePayload bounds how many bytes a single RequestDevDepMsgIn
	// response will carry, regardless of what TransferSize the host asked
	// for. Zero means defaultMaxResponsePayload.
	MaxResponsePayload uint32
}

func (h *Handler) maxResponsePayload() uint32 {
	if h.MaxResponsePayload == 0 {
		return defaultMaxResponsePayload
	}
	return h.MaxResponsePayload
}

// HandleBulkOut processes one complete USBTMC Bulk-OUT message (header
// plus payload, with no alignment padding -- see the package doc comment)
// and returns the bytes to send back on Bulk-IN, or nil if the message
// type has no response (DevDepMsgOut, Trigger).
func (h *Handler) HandleBulkOut(ctx context.Context, msg []byte) ([]byte, error) {
	id, _, ok := wire.DecodeHeaderPrefix(msg)
	if !ok {
		return nil, fmt.Errorf("usbtmcfront: invalid USBTMC header")
	}

	switch id {
	case wire.DevDepMsgOut:
		return h.handleDevDepMsgOut(ctx, msg)
	case wire.RequestDevDepMsgIn:
		return h.handleRequestDevDepMsgIn(ctx, msg)
	case wire.Trigger:
		return nil, h.Instrument.Trigger(ctx)
	default:
		return nil, fmt.Errorf("usbtmcfront: unsupported MessageID %d", id)
	}
}

func (h *Handler) handleDevDepMsgOut(ctx context.Context, msg []byte) ([]byte, error) {
	_, transferSize, eom, ok := wire.DecodeBulkOutHeader(msg)
	if !ok {
		return nil, fmt.Errorf("usbtmcfront: invalid DevDepMsgOut header")
	}
	payload, err := payloadBytes(msg, transferSize)
	if err != nil {
		return nil, err
	}
	return nil, h.Instrument.Write(ctx, payload, eom)
}

func (h *Handler) handleRequestDevDepMsgIn(ctx context.Context, msg []byte) ([]byte, error) {
	tag, transferSize, _, _, ok := wire.DecodeRequestDevDepMsgInHeader(msg)
	if !ok {
		return nil, fmt.Errorf("usbtmcfront: invalid RequestDevDepMsgIn header")
	}

	want := transferSize
	if max := h.maxResponsePayload(); want > max {
		want = max
	}

	buf := make([]byte, want)
	n, eoi, err := h.Instrument.Read(ctx, buf)
	if err != nil {
		return nil, err
	}

	hdr := wire.EncodeDevDepMsgInHeader(tag, uint32(n), eoi, false)
	return append(hdr[:], buf[:n]...), nil
}

// payloadBytes returns msg's payload of the given length, following the
// 12-byte header, bounds-checked against msg's real length rather than
// trusting transferSize.
func payloadBytes(msg []byte, transferSize uint32) ([]byte, error) {
	end := wire.HeaderSize + int(transferSize)
	if end > len(msg) {
		return nil, fmt.Errorf("usbtmcfront: TransferSize %d exceeds message length %d", transferSize, len(msg)-wire.HeaderSize)
	}
	return msg[wire.HeaderSize:end], nil
}
