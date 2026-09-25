//go:build tinygo

package main

import "machine/usb/descriptor"

// This board's USB configuration is CDC (a debug console; interfaces 0-1)
// plus USBTMC/USB488 (interface 2), the maximum TinyGo 0.42.0's own
// machine/usb package supports: usbSetupHandler is a fixed-size
// [usb.NumberOfInterfaces]func(...) array (NumberOfInterfaces == 3), not
// a slice, so a 4th interface is not an option without patching TinyGo
// itself. CDC costs 2 of the 3 slots; USBTMC needs only 1.
//
// USB488 (interface protocol 0x01, not plain USBTMC's 0x00) is
// advertised, but the optional interrupt-IN endpoint USB488 allows for
// asynchronous SRQ notification is not included here -- usbtmcfront
// (see that package) does not implement any control-transfer-based
// request yet, SRQ included, so there is nothing to notify with yet. The
// two bulk endpoints (EP3 IN/OUT) are the required minimum.
const (
	usbtmcInterfaceClass    = 0xfe // ApplicationSpecificBaseClass; matches github.com/gotmc/usbtmc/wire.ApplicationSpecificBaseClass
	usbtmcInterfaceSubClass = 0x03 // USBTMCSubClass
	usbtmcInterfaceProtocol = 0x01 // USB488Protocol
	usbtmcInterfaceNumber   = 2
	usbtmcNumEndpoints      = 2
)

// usbtmcEndpoint is the single endpoint number USBTMC's bulk IN and bulk
// OUT pipes share (as an address, they differ in the direction bit --
// see descriptor.EndpointIN/EndpointOUT). EP1 and EP2 already belong to
// CDC (descriptor.CDC's own layout); this is the next one free, and
// matches the equivalent choice descriptor.MSC makes for its own single
// bulk-only class sharing a descriptor set with CDC.
const usbtmcEndpoint = descriptor.EndpointEP3

// interfaceUSBTMC is USBTMC's interface descriptor (USB 2.0 spec 9.6.5),
// built by hand rather than through descriptor.InterfaceType: that type's
// backing field is unexported, and package machine/usb/descriptor
// provides no exported constructor for it, only for already-built
// interfaces (like InterfaceCDCControl). Byte layout confirmed against
// descriptor.InterfaceType's own setter methods (interface.go).
var interfaceUSBTMC = []byte{
	9, // bLength
	descriptor.TypeInterface,
	usbtmcInterfaceNumber,
	0, // bAlternateSetting
	usbtmcNumEndpoints,
	usbtmcInterfaceClass,
	usbtmcInterfaceSubClass,
	usbtmcInterfaceProtocol,
	0, // iInterface (no string)
}

// usbtmcDescriptor is the full device+configuration descriptor set: CDC's
// own two interfaces, unchanged, plus USBTMC as a third. wTotalLength is
// not set here -- descriptor.Descriptor.Configure recomputes it from the
// real composed length at enumeration time (see machine/usb.go's
// sendDescriptor, which already calls it); only bNumInterfaces needs
// fixing up by hand, since that is not auto-computed.
var usbtmcDescriptor = descriptor.Descriptor{
	Device: descriptor.DeviceCDC.Bytes(),
	Configuration: descriptor.Append([][]byte{
		configurationCDCWithThreeInterfaces(),
		descriptor.InterfaceAssociationCDC.Bytes(),
		descriptor.InterfaceCDCControl.Bytes(),
		descriptor.ClassSpecificCDCHeader.Bytes(),
		descriptor.ClassSpecificCDCCallManagement.Bytes(),
		descriptor.ClassSpecificCDCACM.Bytes(),
		descriptor.ClassSpecificCDCUnion.Bytes(),
		descriptor.EndpointIN(descriptor.EndpointEP1, descriptor.TransferTypeInterrupt, 0x10, 0x10).Bytes(),
		descriptor.InterfaceCDCData.Bytes(),
		descriptor.EndpointOUT(descriptor.EndpointEP2, descriptor.TransferTypeBulk, 0x40, 0x00).Bytes(),
		descriptor.EndpointIN(descriptor.EndpointEP2, descriptor.TransferTypeBulk, 0x40, 0x00).Bytes(),
		interfaceUSBTMC,
		descriptor.EndpointOUT(usbtmcEndpoint, descriptor.TransferTypeBulk, 0x40, 0x00).Bytes(),
		descriptor.EndpointIN(usbtmcEndpoint, descriptor.TransferTypeBulk, 0x40, 0x00).Bytes(),
	}),
}

// configurationCDCWithThreeInterfaces returns a copy -- not a reference,
// which would corrupt the shared package-level descriptor.ConfigurationCDC
// / descriptor.CDC that .Bytes() aliases directly -- of CDC's own
// configuration descriptor with bNumInterfaces (byte offset 4; confirmed
// against descriptor.ConfigurationType.NumInterfaces's own implementation)
// changed from 2 to 3.
func configurationCDCWithThreeInterfaces() []byte {
	c := append([]byte(nil), descriptor.ConfigurationCDC.Bytes()...)
	c[4] = 3
	return c
}
