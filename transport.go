package polemaster

import (
	"errors"
	"fmt"
	"time"
)

const (
	VID       = 0x1618
	PIDBoot   = 0x0940
	PIDCamera = 0x0941
)

const bulkIn = 0x82
const controlTimeout = 1500 * time.Millisecond

var ErrNotFound = errors.New("polemaster: no device attached")

type Device interface {
	PID() uint16
	ControlOut(bRequest uint8, wValue, wIndex uint16, data []byte) error
	ControlIn(bRequest uint8, wValue, wIndex uint16, data []byte) (int, error)
	BulkRead(buf []byte, timeout time.Duration) (int, error)
	ClearHalt() error
	Reset() error
	Close() error
}

func Attached() ([]uint16, error) { return attachedRaw(VID) }

func Open(pid uint16) (Device, error) {
	d, err := openRaw(VID, pid)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func Describe(pid uint16) string {
	switch pid {
	case PIDBoot:
		return "PoleMaster, firmware not loaded"
	case PIDCamera:
		return "PoleMaster, running"
	}
	return fmt.Sprintf("unrecognised QHY device %04x", pid)
}
