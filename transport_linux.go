//go:build linux

package polemaster

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 0x55<<8 | nr }

var (
	usbdevfsControl        = ioc(3, 0, unsafe.Sizeof(usbCtrlTransfer{}))
	usbdevfsBulk           = ioc(3, 2, unsafe.Sizeof(usbBulkTransfer{}))
	usbdevfsClaimInterface = ioc(2, 15, 4)
	usbdevfsClearHalt      = ioc(2, 21, 4)
	usbdevfsReset          = ioc(0, 20, 0)
)

type usbCtrlTransfer struct {
	bRequestType uint8
	bRequest     uint8
	wValue       uint16
	wIndex       uint16
	wLength      uint16
	timeout      uint32
	data         uintptr
}

type usbBulkTransfer struct {
	ep      uint32
	len     uint32
	timeout uint32
	_       uint32
	data    uintptr
}

// Cap each synchronous usbfs transfer at 16 KiB; this is a transport chunk,
// not a frame boundary. The frame reader must assemble successive reads.
const maxBulk = 16 << 10

type usbfsDevice struct {
	f   *os.File
	pid uint16
	mu  sync.Mutex
}

type usbNode struct {
	path     string
	vid, pid uint16
}

func scanUSB(vid uint16) []usbNode {
	paths, _ := filepath.Glob("/dev/bus/usb/*/*")
	var out []usbNode
	for _, p := range paths {
		f, err := os.OpenFile(p, os.O_RDONLY, 0)
		if err != nil {
			continue
		}
		var desc [18]byte
		_, err = f.ReadAt(desc[:], 0)
		f.Close()
		if err != nil {
			continue
		}
		gotVID := uint16(desc[8]) | uint16(desc[9])<<8
		if gotVID != vid {
			continue
		}
		out = append(out, usbNode{path: p, vid: gotVID, pid: uint16(desc[10]) | uint16(desc[11])<<8})
	}
	return out
}

func attachedRaw(vid uint16) ([]uint16, error) {
	nodes := scanUSB(vid)
	out := make([]uint16, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.pid)
	}
	return out, nil
}

func openRaw(vid, pid uint16) (Device, error) {
	for _, n := range scanUSB(vid) {
		if n.pid != pid {
			continue
		}
		f, err := os.OpenFile(n.path, os.O_RDWR, 0)
		if err != nil {
			return nil, fmt.Errorf("polemaster: open %s: %w (a udev rule for vendor %04x grants access)",
				n.path, err, vid)
		}
		d := &usbfsDevice{f: f, pid: pid}
		if err := d.claim(0); err != nil {
			f.Close()
			return nil, fmt.Errorf("polemaster: claim interface 0 on %s: %w", n.path, err)
		}
		return d, nil
	}
	return nil, fmt.Errorf("polemaster: %w with id %04x:%04x", ErrNotFound, vid, pid)
}

func (d *usbfsDevice) PID() uint16 { return d.pid }

func (d *usbfsDevice) ioctl(req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

func (d *usbfsDevice) claim(iface uint32) error {
	return d.ioctl(usbdevfsClaimInterface, unsafe.Pointer(&iface))
}

func (d *usbfsDevice) control(reqType, req uint8, val, idx uint16, data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ct := usbCtrlTransfer{
		bRequestType: reqType, bRequest: req,
		wValue: val, wIndex: idx, wLength: uint16(len(data)),
		timeout: uint32(controlTimeout.Milliseconds()),
	}
	if len(data) > 0 {
		ct.data = uintptr(unsafe.Pointer(&data[0]))
	}
	r1, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), usbdevfsControl,
		uintptr(unsafe.Pointer(&ct)))
	runtime.KeepAlive(data)
	if errno != 0 {
		return 0, fmt.Errorf("polemaster: control req 0x%02x val 0x%04x idx 0x%04x: %w",
			req, val, idx, errno)
	}
	return int(r1), nil
}

func (d *usbfsDevice) ControlOut(req uint8, val, idx uint16, data []byte) error {
	n, err := d.control(0x40, req, val, idx, data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("polemaster: control req 0x%02x sent %d of %d bytes", req, n, len(data))
	}
	return nil
}

func (d *usbfsDevice) ControlIn(req uint8, val, idx uint16, data []byte) (int, error) {
	return d.control(0xc0, req, val, idx, data)
}

func (d *usbfsDevice) BulkRead(buf []byte, timeout time.Duration) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(buf) > maxBulk {
		buf = buf[:maxBulk]
	}
	bt := usbBulkTransfer{
		ep:      bulkIn,
		len:     uint32(len(buf)),
		timeout: uint32(timeout.Milliseconds()),
		data:    uintptr(unsafe.Pointer(&buf[0])),
	}
	r1, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), usbdevfsBulk,
		uintptr(unsafe.Pointer(&bt)))
	runtime.KeepAlive(buf)
	if errno == syscall.ETIMEDOUT {
		return int(r1), nil
	}
	if errno != 0 {
		return 0, fmt.Errorf("polemaster: bulk read: %w", errno)
	}
	return int(r1), nil
}

func (d *usbfsDevice) ClearHalt() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	ep := uint32(bulkIn)
	if err := d.ioctl(usbdevfsClearHalt, unsafe.Pointer(&ep)); err != nil {
		return fmt.Errorf("polemaster: clear stall on 0x%02x: %w", bulkIn, err)
	}
	return nil
}

func (d *usbfsDevice) Reset() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.ioctl(usbdevfsReset, nil); err != nil {
		return fmt.Errorf("polemaster: device reset: %w", err)
	}
	return d.claim(0)
}

func (d *usbfsDevice) Close() error { return d.f.Close() }
