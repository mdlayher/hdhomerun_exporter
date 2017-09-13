package hdhomerun

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/mdlayher/hdhomerun/internal/libhdhomerun"
)

// A DeviceType is a constant indicating the type of an HDHomeRun device,
// such as a tuner or storage unit.
type DeviceType int

// Possible DeviceType values.
const (
	DeviceTypeTuner   = DeviceType(libhdhomerun.DeviceTypeTuner)
	DeviceTypeStorage = DeviceType(libhdhomerun.DeviceTypeStorage)

	// DeviceTypeWildcard is used during discovery to request that all
	// types of devices reply to the request.
	DeviceTypeWildcard = DeviceType(libhdhomerun.DeviceTypeWildcard)
)

// String returns the string representation of a DeviceType.
func (t DeviceType) String() string {
	switch t {
	case DeviceTypeTuner:
		return "tuner"
	case DeviceTypeStorage:
		return "storage"
	case DeviceTypeWildcard:
		return "wildcard"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// DeviceIDWildcard is used during discovery to request that a device
// with any ID reply to the request.
const DeviceIDWildcard = "ffffffff" // libhdhomerun.DeviceIdWildcard

// ParseDeviceID parses an eight character hexadecimal string into its
// byte representation for transmission in a Packet.
func ParseDeviceID(id string) ([]byte, error) {
	idb, err := hex.DecodeString(id)
	if err != nil {
		return nil, err
	}

	if len(idb) != 4 {
		return nil, fmt.Errorf("device ID must be eight hexadecimal characters: %q", id)
	}

	return idb, nil
}

// A Discoverer can discover HDHomeRun devices on a network.
type Discoverer struct {
	deviceType    DeviceType
	deviceID      []byte
	multicastAddr *net.UDPAddr
	localAddr     *net.UDPAddr

	c net.PacketConn
}

// A DiscovererOption is an option which modifies the behavior of a Discoverer.
type DiscovererOption func(d *Discoverer) error

// DiscoverDeviceType requests that a Discoverer only search for devices with
// the specified type.
func DiscoverDeviceType(t DeviceType) DiscovererOption {
	return func(d *Discoverer) error {
		d.deviceType = t
		return nil
	}
}

// DiscoverDeviceID requests that a Discoverer only search for devices with
// the specified ID.
func DiscoverDeviceID(id string) DiscovererOption {
	return func(d *Discoverer) error {
		idb, err := ParseDeviceID(id)
		if err != nil {
			return err
		}

		d.deviceID = idb
		return nil
	}
}

// discoverLocalUDPAddr controls the address used for the Discoverer's local
// UDP listener.
func discoverLocalUDPAddr(network, addr string) DiscovererOption {
	return func(d *Discoverer) error {
		udpAddr, err := net.ResolveUDPAddr(network, addr)
		if err != nil {
			return err
		}

		d.localAddr = udpAddr
		return nil
	}
}

// discoverMulticastUDPAddr controls the address used for the Discoverer's
// multicast UDP discovery requests.
func discoverMulticastUDPAddr(network, addr string) DiscovererOption {
	return func(d *Discoverer) error {
		udpAddr, err := net.ResolveUDPAddr(network, addr)
		if err != nil {
			return err
		}

		d.multicastAddr = udpAddr
		return nil
	}
}

// NewDiscoverer creates a Discoverer that can discover devices using a UDP
// multicast mechanism.  By default, the Discoverer will look for any type
// of device with any ID.
//
// If needed, DiscovererOptions can be provided to modify the behavior of
// the Discoverer.
func NewDiscoverer(options ...DiscovererOption) (*Discoverer, error) {
	d := &Discoverer{
		// Search for any type of device.
		deviceType: DeviceTypeWildcard,
		// Find all physical HDHomeRun devices on our network.
		multicastAddr: &net.UDPAddr{
			IP:   net.IPv4bcast,
			Port: libhdhomerun.DiscoverUdpPort,
		},
		// Bind to any port on all interfaces.
		localAddr: nil,
	}

	// Prepend options which are applied automatically, so user options
	// can override the defaults.
	options = append([]DiscovererOption{
		// Search for any device ID.
		DiscoverDeviceID(DeviceIDWildcard),
	}, options...)

	for _, o := range options {
		if err := o(d); err != nil {
			return nil, err
		}
	}

	c, err := net.ListenUDP("udp", d.localAddr)
	if err != nil {
		return nil, err
	}

	// Discover devices of specified type and ID using the configured
	// multicast group.
	b := mustDiscoverPacket(d.deviceType, d.deviceID)
	if _, err := c.WriteToUDP(b, d.multicastAddr); err != nil {
		_ = c.Close()
		return nil, err
	}

	return &Discoverer{
		c: c,
	}, nil
}

// A retryableError is an error returned during discovery that indicates a
// malformed reply from a device.
type retryableError struct {
	err error
}

// Error implements error.
func (err *retryableError) Error() string {
	return err.err.Error()
}

// Discover discovers HDHomeRun devices over a network.  Discover will block
// indefinitely until a device is found, or the context is canceled.  If
// the context is canceled, an io.EOF error will be returned.
func (d *Discoverer) Discover(ctx context.Context) (*DiscoveredDevice, error) {
	select {
	case <-ctx.Done():
		// If context was already canceled before Discover was called,
		// handle cancelation immediately.
		_ = d.c.Close()

		cerr := ctx.Err()
		switch cerr {
		case context.Canceled, context.DeadlineExceeded:
			// Sentinel value for "done with discovery".
			return nil, io.EOF
		default:
			return nil, cerr
		}
	default:
	}

	for {
		// Keep trying to discover a device until context is canceled or a
		// fatal error is returned.  Malformed device replies will result
		// in a retryableError.
		device, err := d.discover(ctx)
		if err != nil {
			if _, ok := err.(*retryableError); ok {
				continue
			}

			return nil, err
		}

		return device, nil
	}
}

func (d *Discoverer) discover(ctx context.Context) (*DiscoveredDevice, error) {
	// Blocks until a message is received from a device.
	msgC := make(chan struct{})

	// Ensure the cancelation goroutine exits cleanly.
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()

	go func() {
		// When this goroutine exits, expect no additional messages to
		// be sent.
		defer close(msgC)
		defer wg.Done()

		select {
		case <-ctx.Done():
			// Context canceled; clean up and force io.EOF path.
			_ = d.c.Close()
		case <-msgC:
			// Message received or listener error.
		}
	}()

	b := make([]byte, 2048)
	n, addr, err := d.c.ReadFrom(b)
	if err != nil {
		// Depending on whether or not the context was canceled,
		// err might be caused by the goroutine closing the listener.
		cerr := ctx.Err()
		switch cerr {
		case context.Canceled, context.DeadlineExceeded:
			// Sentinel value for "done with discovery".
			return nil, io.EOF
		}

		// We failed to receive a reply; notify the goroutine that
		// it can stop waiting for context cancelation, and clean
		// up the listener.
		msgC <- struct{}{}
		_ = d.c.Close()

		switch cerr {
		case nil:
			// Unhandled listener errors.
			return nil, err
		default:
			// Unhandled context errors.
			return nil, cerr
		}
	}

	// We received a reply successfully; notify the goroutine that
	// it can stop waiting for context cancelation.
	msgC <- struct{}{}

	// There's no guarantee that the message we received is a valid discover
	// reply, so any errors here result in another network read to continue
	// looking for valid devices.

	var p Packet
	if err := (&p).UnmarshalBinary(b[:n]); err != nil {
		return nil, &retryableError{err: err}
	}

	device, err := newDiscoveredDevice(addr.String(), p)
	if err != nil {
		return nil, &retryableError{err: err}
	}

	return device, nil
}

// A DiscoveredDevice is a device encountered during discovery.  Its network
// address can be used with Dial to initiate a direct connection to a device.
type DiscoveredDevice struct {
	// ID is the unique ID of this device.
	ID string

	// Addr is the network address of this device.
	Addr string

	// Type is the type of device discovered, such as a tuner or storage unit.
	Type DeviceType

	// URL, if available, is the URL for the device's web UI.
	URL *url.URL

	// Tuners is the number of TV tuners available to the device.
	Tuners int
}

// newDiscoveredDevice creates a DiscoveredDevice using the data from a
// discover reply packet.
func newDiscoveredDevice(addr string, p Packet) (*DiscoveredDevice, error) {
	if p.Type != libhdhomerun.TypeDiscoverRpy {
		return nil, fmt.Errorf("expected discover reply, but got %#x", p.Type)
	}

	device := &DiscoveredDevice{
		Addr: addr,
	}

	if err := device.parseTags(p.Tags); err != nil {
		return nil, err
	}

	if device.Type == 0 {
		return nil, errors.New("no device type found in discover reply")
	}
	if device.ID == "" {
		return nil, errors.New("no device ID found in discover reply")
	}

	return device, nil
}

// parseTags parses a slice of Tags for fields that belong to a DiscoveredDevice.
func (d *DiscoveredDevice) parseTags(tags []Tag) error {
	for _, t := range tags {
		switch t.Type {
		case libhdhomerun.TagDeviceType:
			if l := len(t.Data); l != 4 {
				return fmt.Errorf("unexpected device type length in discover reply: %d", l)
			}

			d.Type = DeviceType(binary.BigEndian.Uint32(t.Data))
		case libhdhomerun.TagDeviceId:
			if l := len(t.Data); l != 4 {
				return fmt.Errorf("unexpected device ID length in discover reply: %d", l)
			}

			d.ID = hex.EncodeToString(t.Data)
		case libhdhomerun.TagBaseUrl:
			u, err := url.Parse(string(t.Data))
			if err != nil {
				return err
			}

			d.URL = u
		case libhdhomerun.TagTunerCount:
			if l := len(t.Data); l != 1 {
				return fmt.Errorf("unexpected tuner count length in discover reply: %d", l)
			}

			d.Tuners = int(t.Data[0])
		default:
			// TODO(mdlayher): handle additional tags if needed
		}
	}

	return nil
}

// mustDiscoverPacket produces the bytes for a device discovery packet,
// using the specified or wildcard device type and device ID. It panics
// if any errors occur while creating the discovery packet.
func mustDiscoverPacket(typ DeviceType, id []byte) []byte {
	if len(id) != 4 {
		panicf("device ID must be exactly 4 bytes: %v", id)
	}

	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(typ))

	p := &Packet{
		Type: libhdhomerun.TypeDiscoverReq,
		Tags: []Tag{
			{
				Type: libhdhomerun.TagDeviceType,
				Data: b,
			},
			{
				Type: libhdhomerun.TagDeviceId,
				Data: id,
			},
		},
	}

	pb, err := p.MarshalBinary()
	if err != nil {
		panicf("failed to marshal discover packet: %v", err)
	}

	return pb
}

// panicf is a convenience function for panic with fmt.Sprintf.
func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}
