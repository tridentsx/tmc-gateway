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

# go mod tidy + verify.
tidy:
  go mod tidy
  go mod verify
