module github.com/tridentsx/tmc-gateway

go 1.25.0

require (
	github.com/gotmc/usbtmc v0.0.0-00010101000000-000000000000
	github.com/tridentsx/hislip v0.0.0-20260924231716-819d5e59c761
)

// github.com/gotmc/usbtmc's own go.mod keeps that module path deliberately
// (this fork is meant for an eventual upstream PR, not a permanent rename --
// see github.com/tridentsx/usbtmc's README), so depending on our fork's new
// wire subpackage needs a replace directive rather than a plain require.
replace github.com/gotmc/usbtmc => github.com/tridentsx/usbtmc v0.15.2-0.20260925045406-b7c86a69fe58
