package hdhomerun

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// A StopReason is a reason why a tuner's network stream has stopped.
type StopReason int

// Possible StopReason values.
const (
	StopReasonNotStopped          StopReason = 0
	StopReasonIntentional         StopReason = 1
	StopReasonICMPReject          StopReason = 2
	StopReasonConnectionLoss      StopReason = 3
	StopReasonHTTPConnectionClose StopReason = 4
)

// A Tuner is an HDHomeRun TV tuner.  The Index field specifies which tuner
// will be queried.  Tuners should be constructed using the Tuner method of
// the Client type.
type Tuner struct {
	Index int
	c     *Client
}

// Debug retrieves a variety of debugging information about the Tuner, specified
// by its index.
func (t *Tuner) Debug() (*TunerDebug, error) {
	b, err := t.query("debug")
	if err != nil {
		return nil, err
	}

	debug := new(TunerDebug)
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		// Trim off any trailing null bytes.
		if err := debug.parse(bytesStr(s.Bytes())); err != nil {
			return nil, err
		}
	}

	return debug, s.Err()
}

// query performs a Client query prefixed with this Tuner's base path.
func (t *Tuner) query(query string) ([]byte, error) {
	base := fmt.Sprintf("/tuner%d/", t.Index)
	return t.c.Query(path.Join(base, query))
}

// TunerDebug contains debugging information about an HDHomeRun TV tuner.
//
// If information about a particular component is not available, the
// field's value will be nil.
//
// Information about particular fields can be found in the HDHomeRun
// Development Guide:
// https://www.silicondust.com/hdhomerun/hdhomerun_development.pdf.
type TunerDebug struct {
	Tuner           *TunerStatus
	Device          *DeviceStatus
	CableCARD       *CableCARDStatus
	TransportStream *TransportStreamStatus
	Network         *NetworkStatus
}

// TODO(mdlayher): determine if Channel, Lock, and Debug fields merit
// their own special types.

// TunerStatus is the status of an HDHomeRun tuner.
type TunerStatus struct {
	Channel              string
	Lock                 string
	SignalStrength       int
	SignalToNoiseQuality int
	SymbolErrorQuality   int
	Debug                string
}

// DeviceStatus is the status of the tuner while processing a stream.
type DeviceStatus struct {
	BitsPerSecond int
	Resync        int
	Overflow      int
}

// CableCARDStatus is the status of a CableCARD, if one is present
// in the device.
type CableCARDStatus struct {
	BitsPerSecond int
	Resync        int
	Overflow      int
}

// TransportStreamStatus is the status of an incoming video stream
// from the tuner.
type TransportStreamStatus struct {
	BitsPerSecond   int
	TransportErrors int
	CRCErrors       int
}

// NetworkStatus is the status of an outgoing network stream from the
// tuner.
type NetworkStatus struct {
	PacketsPerSecond int
	Errors           int
	Stop             StopReason
}

// parse parses a tuner debug status line.
func (td *TunerDebug) parse(s string) error {
	ss := strings.Fields(s)
	switch {
	case len(ss) == 0:
		// Probably an empty line.
		return nil
	case len(ss) < 2:
		return fmt.Errorf("malformed tuner status line: %q", s)
	}

	// Assume all fields after the first are the key/value pairs.
	kvs, err := kvStrings(ss[1:])
	if err != nil {
		return err
	}

	switch ss[0] {
	case "tun:":
		return td.parseTuner(kvs)
	case "cc:":
		return td.parseCableCARD(kvs)
	case "dev:":
		return td.parseDevice(kvs)
	case "ts:":
		return td.parseTransportStream(kvs)
	case "net:":
		return td.parseNetwork(kvs)
	}

	// A key we don't recognize.
	return nil
}

// parseTuner parses a tuner status line.
func (td *TunerDebug) parseTuner(kvs [][2]string) error {
	cc := new(TunerStatus)

	for _, kv := range kvs {
		switch kv[0] {
		case "ch":
			cc.Channel = kv[1]
		case "lock":
			cc.Lock = kv[1]
		case "dbg":
			cc.Debug = kv[1]
		}

		// Expect some fields to be numerical. Each field added to this
		// switch must also be added to the switch below.
		switch kv[0] {
		case "ss", "snq", "seq":
		default:
			// Field not recognized; keep parsing.
			continue
		}

		v, err := strconv.Atoi(kv[1])
		if err != nil {
			return err
		}

		switch kv[0] {
		case "ss":
			cc.SignalStrength = v
		case "snq":
			cc.SignalToNoiseQuality = v
		case "seq":
			cc.SymbolErrorQuality = v
		default:
			// Rationale for panic: if both switch statements aren't kept in
			// sync, this is a clear programming error.
			panicf("unhandled numerical tuner status key: %q", kv[0])
		}
	}

	td.Tuner = cc
	return nil
}

// parseCableCARD parses a CableCARD status line.
func (td *TunerDebug) parseCableCARD(kvs [][2]string) error {
	cc := new(CableCARDStatus)

	for _, kv := range kvs {
		v, err := strconv.Atoi(kv[1])
		if err != nil {
			return err
		}

		switch kv[0] {
		case "bps":
			cc.BitsPerSecond = v
		case "resync":
			cc.Resync = v
		case "overflow":
			cc.Overflow = v
		}
	}

	td.CableCARD = cc
	return nil
}

// parseDevice parses a device status line.
func (td *TunerDebug) parseDevice(kvs [][2]string) error {
	dev := new(DeviceStatus)

	for _, kv := range kvs {
		v, err := strconv.Atoi(kv[1])
		if err != nil {
			return err
		}

		switch kv[0] {
		case "bps":
			dev.BitsPerSecond = v
		case "resync":
			dev.Resync = v
		case "overflow":
			dev.Overflow = v
		}
	}

	td.Device = dev
	return nil
}

// parseTransportStream parses a transport status line.
func (td *TunerDebug) parseTransportStream(kvs [][2]string) error {
	ts := new(TransportStreamStatus)

	for _, kv := range kvs {
		v, err := strconv.Atoi(kv[1])
		if err != nil {
			return err
		}

		switch kv[0] {
		case "bps":
			ts.BitsPerSecond = v
		case "te":
			ts.TransportErrors = v
		case "crc":
			ts.CRCErrors = v
		}
	}

	td.TransportStream = ts
	return nil
}

// parseNetwork parses a network status line.
func (td *TunerDebug) parseNetwork(kvs [][2]string) error {
	net := new(NetworkStatus)

	for _, kv := range kvs {
		v, err := strconv.Atoi(kv[1])
		if err != nil {
			return err
		}

		switch kv[0] {
		case "pps":
			net.PacketsPerSecond = v
		case "err":
			net.Errors = v
		case "stop":
			net.Stop = StopReason(v)
		}
	}

	td.Network = net
	return nil
}

// kvStrings parses a slice of strings in key=value format into a slice
// of key/value pairs.
func kvStrings(ss []string) ([][2]string, error) {
	kvs := make([][2]string, 0, len(ss))
	for _, s := range ss {
		kv := strings.Split(s, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid key=value pair: %q", s)
		}

		var arr [2]string
		copy(arr[:], kv)

		kvs = append(kvs, arr)
	}

	return kvs, nil
}
