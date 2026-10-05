# go fmt + go vet.
check:
  go fix ./...
  go fmt ./...
  go vet ./...

# Unit tests (runs check first).
unit: check
  go test ./... -cover

# Compile cmd/tinygocheck for the RP2350 target, exercising gpib and
# hislipfront's real dependency graph (which pulls in hislip/server and
# hislip/protocol) under TinyGo rather than only mainline go build.
# Requires tinygo.
tinygo:
  tinygo build -target=pico2 -o /tmp/tmc-gateway-tinygocheck.elf ./cmd/tinygocheck

# Build the real (skeleton) firmware as a flashable UF2. Requires tinygo.
# Bakes the git commit (+ "-dirty" if uncommitted changes exist) and a UTC
# build timestamp into cmd/firmware's firmwareVersion var, so the "version"
# debug-console command (or the boot line) can tell exactly which build is
# running on a given board -- see cmd/firmware/debug.go.
firmware:
  #!/usr/bin/env bash
  set -euo pipefail
  commit=$(git rev-parse --short HEAD)
  if ! git diff --quiet || ! git diff --cached --quiet; then
    commit="${commit}-dirty"
  fi
  built=$(date -u +%Y%m%dT%H%M%SZ)
  tinygo build -target=pico2 \
    -ldflags "-X main.firmwareVersion=${commit}-${built}" \
    -o /tmp/tmc-gateway-firmware.uf2 ./cmd/firmware

# Host-side TUI for cmd/firmware's CDC debug console -- a menu of known
# commands plus a raw-command box, instead of hand-typing into
# screen/picocom/pyserial. Pass port="/dev/..." to override auto-detect.
debugconsole port="":
  go run ./cmd/debugconsole {{ if port != "" { "-port " + port } else { "" } }}

# go mod tidy + verify.
tidy:
  go mod tidy
  go mod verify
