# tmc-gateway

A PoE Ethernet- and USB-attached GPIB instrument gateway: one small board,
plugged directly into an instrument's IEEE-488 connector, that exposes it to
the network and to USB simultaneously.

## What it does

One RP2354A-based adapter per GPIB instrument, acting as that instrument's
GPIB System Controller and Controller-in-Charge, reachable over:

- **[HiSLIP][hislip]** (IVI-6.1) over Ethernet/PoE, TCP port 4880
- **USBTMC** and its **USB488** subclass over USB
- Raw SCPI over TCP port 5025 (planned; not yet built)

The central architectural rule, carried over from the design specification:

> HiSLIP, USBTMC/USB488, raw SCPI, and any future management interface never
> manipulate GPIB pins directly. They submit operations to one serialized
> GPIB engine.

That engine is [`gpib.Instrument`](gpib/instrument.go) — see
[Software](#software) below.

## Hardware

| | |
|---|---|
| MCU | RP2354A (RP2350 family, 2 MB integrated flash), QFN-60 |
| Ethernet | WIZnet W5500 (SPI) |
| PoE | DP9900LPB isolated PD module, IEEE 802.3af, with a USB-C alternate power path (§30.1) |
| GPIB transceivers | TI SN75160B (data bus) + SN75161B (management bus) |
| GPIB connector | 24-pin IEEE-488, NorComp 112-024-113R001 |
| USB | USB-C (USBTMC/USB488, plus alternate power input) |

Board is 62×68 mm, 4 copper layers. Schematic is complete and verified —
confirmed by actually running the checks below, not just citing an old note:
`scripts/netlist-check.py` reports **124 checks, 0 failures**; ERC reports 3
`pin_not_driven` errors, all on W5500 (`U2`) SPI inputs (`SCS`/`SCLK`/`MOSI`)
flagged because the driving RP2354A pins are bidirectional across a
hierarchical sheet link — a severity-policy question, not a wiring defect.
PCB has all 93 footprints placed but is **not yet routed**.

- [`docs/design-spec.md`](docs/design-spec.md) — the master design
  specification. One document by design: Part I is the HiSLIP library
  ([tridentsx/hislip][hislip], developed in that separate repository), Part
  II is this hardware, Part III is this firmware, and Parts IV–VII
  (milestones, acceptance criteria, design decisions, risk register) span
  all three. Source comments in `hislip` cite it by section, e.g. "§5.1".
- [`docs/netlist-spec.md`](docs/netlist-spec.md) / [`docs/netlist-spec.nets`](docs/netlist-spec.nets) —
  the canonical net table `scripts/netlist-check.py` verifies the exported
  schematic netlist against.
- [`docs/rp2354a-pinmap.md`](docs/rp2354a-pinmap.md) — the RP2354A GPIO
  assignment: 8 data lines and the DAV/NDAC/NRFD handshake lines suggested
  for PIO, ATN/SRQ/REN/IFC/EOI as plain GPIO, SPI0 to the W5500.

## Software

```text
gpib/          the Instrument abstraction every front end drives (no
                firmware, no bus driver -- just the interface)
hislipfront/    adapts gpib.Instrument to tridentsx/hislip's server.Device
```

`gpib.Instrument` is deliberately not a copy of `hislip/server.Device`,
despite both stating the identical "must not know it's GPIB" principle:
`RemoteEnable`, `LocalLockout`, and `GoToLocal` are three separate methods
here, because USB488's own control requests (`REN_CONTROL`, `GO_TO_LOCAL`,
`LOCAL_LOCKOUT`) are three separate requests, not HiSLIP's one combined
`RemoteLocal(mode)` call. Both front ends translate onto the same granular
primitives instead of one needing to reconstruct a combined mode it never
actually received.

### Status

| Piece | State |
|---|---|
| `gpib.Instrument` abstraction | Defined |
| `hislipfront` (HiSLIP → `gpib.Instrument`) | Implemented, unit-tested |
| USBTMC/USB488 front end | Not started — blocked on device-side wire codec (below) |
| GPIB bus driver (the concrete `gpib.Instrument`) | Not started |
| Raw SCPI front end | Not started |
| TinyGo build verification | Not done — TinyGo isn't installed anywhere this has been built yet |

Two real, non-mechanical pieces of protocol work are still open before the
USB side or the bus driver can start:

- **Device-side USBTMC wire codec.** [tridentsx/usbtmc][usbtmc]'s new
  `wire` subpackage only has the *client* direction: encoding Bulk-OUT
  headers, decoding Bulk-IN responses. A device needs the mirror image
  (decode Bulk-OUT, encode a `DevDepMsgIn` Bulk-IN response), which exists
  in no form yet, private or exported.
- **The real IEEE-488.1 three-wire handshake and ATN-based addressing**,
  needed by the bus driver. Nothing here has been verified against real
  GPIB hardware yet; treat any bus-timing claim in this repo's history as
  reasoned from the spec text, not measured.

## Related repositories

- [tridentsx/hislip][hislip] — the HiSLIP protocol/server/client library
  this board's Ethernet-side front end is built on. Intended, once
  finished, to be offered to the `gotmc` organisation.
- [tridentsx/usbtmc][usbtmc] — a fork of [gotmc/usbtmc][gotmc-usbtmc]
  adding a driver backed by [tridentsx/go-usb][go-usb] (no cgo, no
  libusb), intended to be contributed upstream. Its new `wire` subpackage
  is the USBTMC wire-format vocabulary this board's USB-side front end
  will build on.
- [tridentsx/go-usb][go-usb] — the pure-Go USB backend behind the
  `usbtmc` driver above.

## Building what exists today

```sh
go build ./...
go vet ./...
go test ./...
```

```sh
kicad-cli sch export netlist --format kicadsexpr -o build/adapter.net hardware/adapter.kicad_sch
python3 scripts/netlist-check.py docs/netlist-spec.nets build/adapter.net   # expect 131/0
kicad-cli sch erc --severity-error -o /tmp/erc.rpt hardware/adapter.kicad_sch
```

[hislip]: https://github.com/tridentsx/hislip
[usbtmc]: https://github.com/tridentsx/usbtmc
[gotmc-usbtmc]: https://github.com/gotmc/usbtmc
[go-usb]: https://github.com/tridentsx/go-usb
