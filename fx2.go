package polemaster

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mikefsq/polemaster/firmware"
)

const (
	fx2RAMWrite = 0xa0
	fx2CPUCS    = 0xe600
)

type hexRecord struct {
	addr uint16
	data []byte
}

func parseHex(text string) ([]hexRecord, error) {
	var out []hexRecord
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line[0] != ':' {
			return nil, fmt.Errorf("polemaster: hex line %d does not start with ':'", i+1)
		}
		raw, err := hex.DecodeString(line[1:])
		if err != nil {
			return nil, fmt.Errorf("polemaster: hex line %d: %w", i+1, err)
		}
		if len(raw) < 4 {
			return nil, fmt.Errorf("polemaster: hex line %d is too short", i+1)
		}
		n, addr, typ := int(raw[0]), uint16(raw[1])<<8|uint16(raw[2]), raw[3]
		body := raw[4:]
		switch len(body) {
		case n:
			// Checksum-free records are intentionally accepted alongside Intel HEX;
			// a checksum, when present, must still validate.
		case n + 1:
			var sum byte
			for _, b := range raw {
				sum += b
			}
			if sum != 0 {
				return nil, fmt.Errorf("polemaster: hex line %d has a bad checksum", i+1)
			}
			body = body[:n]
		default:
			return nil, fmt.Errorf("polemaster: hex line %d declares %d bytes and carries %d",
				i+1, n, len(body))
		}
		switch typ {
		case 0x00:
			out = append(out, hexRecord{addr: addr, data: body})
		case 0x01:
			return out, nil
		default:
			return nil, fmt.Errorf("polemaster: hex line %d has unsupported record type %02x", i+1, typ)
		}
	}
	return out, nil
}

type Record struct {
	Addr uint16
	Data []byte
}

func FirmwareRecords() ([]Record, error) {
	recs, err := parseHex(firmware.PoleMaster)
	if err != nil {
		return nil, err
	}
	out := make([]Record, len(recs))
	for i, r := range recs {
		out[i] = Record{Addr: r.addr, Data: r.data}
	}
	return out, nil
}

func halt8051(d Device, halt bool) error {
	v := byte(0)
	if halt {
		v = 1
	}
	return d.ControlOut(fx2RAMWrite, fx2CPUCS, 0, []byte{v})
}

func writeRecords(d Device, recs []hexRecord) error {
	for _, r := range recs {
		if err := d.ControlOut(fx2RAMWrite, r.addr, 0, r.data); err != nil {
			return fmt.Errorf("polemaster: write %d bytes to 0x%04x: %w", len(r.data), r.addr, err)
		}
	}
	return nil
}

func LoadFirmware(d Device) error { return LoadImage(d, firmware.PoleMaster) }

func LoadImage(d Device, image string) error {
	cam, err := parseHex(image)
	if err != nil {
		return err
	}
	if err := halt8051(d, true); err != nil {
		return fmt.Errorf("polemaster: halt the 8051: %w", err)
	}
	time.Sleep(time.Second)

	if err := writeRecords(d, cam); err != nil {
		return fmt.Errorf("polemaster: download the camera firmware: %w", err)
	}
	if err := halt8051(d, false); err != nil {
		return fmt.Errorf("polemaster: run the camera firmware: %w", err)
	}
	return nil
}

// Exercise both control-transfer directions without changing the register value.
// Enumeration alone does not establish that the camera is ready for commands.
func probe(d Device) error {
	if _, err := d.ControlIn(reqIdentity, 0x10, 0, make([]byte, 16)); err != nil {
		return err
	}
	c := Attach(d)
	v, err := c.ReadReg(regDigitalTest)
	if err != nil {
		return err
	}
	return c.WriteReg(regDigitalTest, v)
}

func WaitForCamera(within time.Duration) (Device, error) {
	deadline := time.Now().Add(within)
	var last error
	for {
		d, err := Open(PIDCamera)
		if err == nil {
			if last = probe(d); last == nil {
				return d, nil
			}
			d.Close()
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("polemaster: %04x:%04x did not answer within %s: %w",
				VID, PIDCamera, within, last)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func Connect() (Device, error) {
	pids, err := Attached()
	if err != nil {
		return nil, err
	}
	var boot, cam bool
	for _, p := range pids {
		switch p {
		case PIDBoot:
			boot = true
		case PIDCamera:
			cam = true
		}
	}
	switch {
	case cam:
		d, err := Open(PIDCamera)
		if err != nil {
			return nil, err
		}
		if probe(d) == nil {
			return d, nil
		}
		// An interrupted capture can wedge control transfers as well as bulk IN.
		// A USB reset can recover the device while preserving its RAM firmware.
		d.Reset()
		d.Close()
		return WaitForCamera(15 * time.Second)
	case boot:
		d, err := Open(PIDBoot)
		if err != nil {
			return nil, err
		}
		err = LoadFirmware(d)
		d.Close()
		if err != nil {
			return nil, err
		}
		return WaitForCamera(15 * time.Second)
	}
	return nil, fmt.Errorf("polemaster: %w (looked for %04x:%04x and %04x:%04x)",
		ErrNotFound, VID, PIDBoot, VID, PIDCamera)
}
