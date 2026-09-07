//go:build darwin

package polemaster

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <IOKit/IOCFPlugIn.h>
#include <IOKit/usb/IOUSBLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef struct {
    IOUSBDeviceInterface**    dev;
    IOUSBInterfaceInterface** intf;
    UInt8 pipe;      // pipe ref of endpoint 0x82; 0 when the device carries none
    int numEndpoints;
    int maxPacket;   // wMaxPacketSize of that pipe
} pm_dev;

static IOUSBDeviceInterface** pm_device_interface(io_service_t svc) {
    IOCFPlugInInterface** plugin = NULL; SInt32 score;
    if (IOCreatePlugInInterfaceForService(svc, kIOUSBDeviceUserClientTypeID,
            kIOCFPlugInInterfaceID, &plugin, &score) != KERN_SUCCESS || !plugin)
        return NULL;
    IOUSBDeviceInterface** dev = NULL;
    (*plugin)->QueryInterface(plugin, CFUUIDGetUUIDBytes(kIOUSBDeviceInterfaceID), (LPVOID*)&dev);
    (*plugin)->Release(plugin);
    return dev;
}

static int pm_open_interface(IOUSBDeviceInterface** dev, IOUSBInterfaceInterface*** outIntf,
                             UInt8* outPipe, int* outNumEp, int* outMaxPacket) {
    IOUSBFindInterfaceRequest req;
    req.bInterfaceClass    = kIOUSBFindInterfaceDontCare;
    req.bInterfaceSubClass = kIOUSBFindInterfaceDontCare;
    req.bInterfaceProtocol = kIOUSBFindInterfaceDontCare;
    req.bAlternateSetting  = kIOUSBFindInterfaceDontCare;
    io_iterator_t it;
    if ((*dev)->CreateInterfaceIterator(dev, &req, &it) != kIOReturnSuccess) return -1;
    io_service_t usbIf = IOIteratorNext(it);
    IOObjectRelease(it);
    if (!usbIf) return -2;

    int rc = -3;
    IOCFPlugInInterface** pl = NULL; SInt32 score;
    if (IOCreatePlugInInterfaceForService(usbIf, kIOUSBInterfaceUserClientTypeID,
            kIOCFPlugInInterfaceID, &pl, &score) == KERN_SUCCESS && pl) {
        IOUSBInterfaceInterface** intf = NULL;
        (*pl)->QueryInterface(pl, CFUUIDGetUUIDBytes(kIOUSBInterfaceInterfaceID), (LPVOID*)&intf);
        (*pl)->Release(pl);
        if (intf) {
            if ((*intf)->USBInterfaceOpen(intf) == kIOReturnSuccess) {
                UInt8 n = 0; (*intf)->GetNumEndpoints(intf, &n);
                *outNumEp = n;
                for (UInt8 i = 1; i <= n; i++) {
                    UInt8 dir, num, tt, interval; UInt16 maxp;
                    if ((*intf)->GetPipeProperties(intf, i, &dir, &num, &tt, &maxp, &interval)
                            == kIOReturnSuccess && dir == kUSBIn && tt == kUSBBulk && num == 2) {
                        *outPipe = i;
                        *outMaxPacket = maxp;
                        break;
                    }
                }
                *outIntf = intf;
                rc = 0;
            } else {
                (*intf)->Release(intf);
                rc = -4;
            }
        }
    }
    IOObjectRelease(usbIf);
    return rc;
}

static int pm_reg_u32(io_service_t svc, CFStringRef key, uint32_t* out) {
    CFTypeRef v = IORegistryEntryCreateCFProperty(svc, key, kCFAllocatorDefault, 0);
    if (!v) return -1;
    int ok = 0;
    if (CFGetTypeID(v) == CFNumberGetTypeID())
        ok = CFNumberGetValue((CFNumberRef)v, kCFNumberSInt32Type, out);
    CFRelease(v);
    return ok ? 0 : -1;
}

static int pm_ids(io_service_t svc, uint32_t* vid, uint32_t* pid) {
    return (pm_reg_u32(svc, CFSTR("idVendor"), vid) == 0 &&
            pm_reg_u32(svc, CFSTR("idProduct"), pid) == 0) ? 0 : -1;
}

static int pm_list(uint16_t vid, uint16_t* out, int max) {
    CFMutableDictionaryRef match = IOServiceMatching(kIOUSBDeviceClassName);
    if (!match) return -1;
    io_iterator_t iter;
    if (IOServiceGetMatchingServices(kIOMainPortDefault, match, &iter) != KERN_SUCCESS) return -2;
    int n = 0;
    io_service_t svc;
    while (n < max && (svc = IOIteratorNext(iter))) {
        uint32_t gotVID = 0, gotPID = 0;
        if (pm_ids(svc, &gotVID, &gotPID) == 0 && gotVID == vid) out[n++] = (uint16_t)gotPID;
        IOObjectRelease(svc);
    }
    IOObjectRelease(iter);
    return n;
}

static int pm_open(uint16_t vid, uint16_t pid, pm_dev* out) {
    CFMutableDictionaryRef match = IOServiceMatching(kIOUSBDeviceClassName);
    if (!match) return -1;

    io_iterator_t iter;
    if (IOServiceGetMatchingServices(kIOMainPortDefault, match, &iter) != KERN_SUCCESS) return -2;

    int rc = -3;
    io_service_t svc;
    while (rc != 0 && (svc = IOIteratorNext(iter))) {
        uint32_t gotVID = 0, gotPID = 0;
        if (pm_ids(svc, &gotVID, &gotPID) != 0 || gotVID != vid || gotPID != pid) {
            IOObjectRelease(svc);
            continue;
        }
        IOUSBDeviceInterface** dev = pm_device_interface(svc);
        if (dev) {
            if ((*dev)->USBDeviceOpen(dev) == kIOReturnSuccess) {
                (*dev)->SetConfiguration(dev, 1);
                IOUSBInterfaceInterface** intf = NULL;
                UInt8 pipe = 0;
                int numEp = 0, maxp = 0;
                if (pm_open_interface(dev, &intf, &pipe, &numEp, &maxp) == 0) {
                    out->dev = dev; out->intf = intf; out->pipe = pipe;
                    out->numEndpoints = numEp; out->maxPacket = maxp;
                    rc = 0;
                } else {
                    (*dev)->USBDeviceClose(dev);
                    (*dev)->Release(dev);
                    rc = -5;
                }
            } else {
                (*dev)->Release(dev);
                rc = -4;
            }
        }
        IOObjectRelease(svc);
    }
    IOObjectRelease(iter);
    return rc;
}

static int pm_control(pm_dev* d, uint8_t reqType, uint8_t req, uint16_t val, uint16_t idx,
                      void* data, uint16_t len, uint32_t* done, uint32_t timeoutMs) {
    IOUSBDevRequestTO r;
    r.bmRequestType = reqType;
    r.bRequest = req;
    r.wValue = val;
    r.wIndex = idx;
    r.wLength = len;
    r.pData = data;
    r.wLenDone = 0;
    r.noDataTimeout = timeoutMs;
    r.completionTimeout = timeoutMs;

    IOReturn kr = kIOReturnSuccess;
    for (int attempt = 0; attempt < 4; attempt++) {
        r.wLenDone = 0;
        kr = (*d->dev)->DeviceRequestTO(d->dev, &r);
        if (kr == kIOReturnSuccess) break;
        if (kr == kIOReturnNoDevice || kr == kIOReturnNotResponding) break;
        usleep(2000);
    }
    if (done) *done = r.wLenDone;
    return (int)kr;
}

static int pm_bulk_read(pm_dev* d, void* buf, uint32_t* len, uint32_t timeoutMs) {
    if (!d->pipe) return kIOReturnNoDevice;
    UInt32 want = *len, n = want;
    IOReturn kr = (*d->intf)->ReadPipeTO(d->intf, d->pipe, buf, &n, timeoutMs, timeoutMs);
    *len = n;
    if (kr == kIOUSBTransactionTimeout && n > 0) return kIOReturnSuccess;

    (void)want;
    return (int)kr;
}

static int pm_clear_stall(pm_dev* d) {
    if (!d->pipe) return kIOReturnNoDevice;
    return (int)(*d->intf)->ClearPipeStallBothEnds(d->intf, d->pipe);
}

static int pm_abort_pipe(pm_dev* d) {
    if (!d->pipe) return kIOReturnNoDevice;
    (*d->intf)->AbortPipe(d->intf, d->pipe);
    return (int)(*d->intf)->ResetPipe(d->intf, d->pipe);
}

static int pm_reset_device(pm_dev* d) {
    if (!d->dev) return kIOReturnNoDevice;
    return (int)(*d->dev)->ResetDevice(d->dev);
}

static void pm_close(pm_dev* d) {
    if (d->intf) { (*d->intf)->USBInterfaceClose(d->intf); (*d->intf)->Release(d->intf); d->intf = NULL; }
    if (d->dev)  { (*d->dev)->USBDeviceClose(d->dev);      (*d->dev)->Release(d->dev);   d->dev = NULL; }
}
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

type darwinDevice struct {
	c   C.pm_dev
	pid uint16
	mu  sync.Mutex
}

func attachedRaw(vid uint16) ([]uint16, error) {
	var buf [32]C.uint16_t
	n := C.pm_list(C.uint16_t(vid), &buf[0], C.int(len(buf)))
	if n < 0 {
		return nil, fmt.Errorf("polemaster: IOKit device match failed (%d)", int(n))
	}
	out := make([]uint16, int(n))
	for i := range out {
		out[i] = uint16(buf[i])
	}
	return out, nil
}

func openRaw(vid, pid uint16) (Device, error) {
	d := &darwinDevice{pid: pid}
	switch rc := C.pm_open(C.uint16_t(vid), C.uint16_t(pid), &d.c); rc {
	case 0:
	case -3:
		return nil, fmt.Errorf("polemaster: %w with id %04x:%04x", ErrNotFound, vid, pid)
	case -4:
		return nil, fmt.Errorf("polemaster: %04x:%04x found but another process holds it", vid, pid)
	case -5:
		return nil, fmt.Errorf("polemaster: %04x:%04x found but its interface could not be claimed", vid, pid)
	default:
		return nil, fmt.Errorf("polemaster: opening %04x:%04x failed (%d)", vid, pid, int(rc))
	}
	return d, nil
}

func (d *darwinDevice) PID() uint16 { return d.pid }

func (d *darwinDevice) Describe() string {
	return fmt.Sprintf("%d endpoints, bulk-IN pipe %d, max packet %d",
		int(d.c.numEndpoints), int(d.c.pipe), int(d.c.maxPacket))
}

func (d *darwinDevice) control(reqType, req uint8, val, idx uint16, data []byte, timeout time.Duration) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var p unsafe.Pointer
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	var done C.uint32_t
	kr := C.pm_control(&d.c, C.uint8_t(reqType), C.uint8_t(req), C.uint16_t(val), C.uint16_t(idx),
		p, C.uint16_t(len(data)), &done, C.uint32_t(timeout.Milliseconds()))
	if kr != 0 {
		return int(done), fmt.Errorf("polemaster: control req 0x%02x val 0x%04x idx 0x%04x: IOKit 0x%08x",
			req, val, idx, uint32(kr))
	}
	return int(done), nil
}

func (d *darwinDevice) ControlOut(req uint8, val, idx uint16, data []byte) error {
	n, err := d.control(0x40, req, val, idx, data, controlTimeout)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("polemaster: control req 0x%02x sent %d of %d bytes", req, n, len(data))
	}
	return nil
}

func (d *darwinDevice) ControlIn(req uint8, val, idx uint16, data []byte) (int, error) {
	return d.control(0xc0, req, val, idx, data, controlTimeout)
}

func (d *darwinDevice) BulkRead(buf []byte, timeout time.Duration) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := C.uint32_t(len(buf))
	kr := C.pm_bulk_read(&d.c, unsafe.Pointer(&buf[0]), &n, C.uint32_t(timeout.Milliseconds()))
	if kr != 0 {
		return int(n), fmt.Errorf("polemaster: bulk read of %d bytes returned %d: IOKit 0x%08x",
			len(buf), int(n), uint32(kr))
	}
	return int(n), nil
}

func (d *darwinDevice) ClearHalt() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	C.pm_abort_pipe(&d.c)
	if kr := C.pm_clear_stall(&d.c); kr != 0 {
		return fmt.Errorf("polemaster: clear stall on 0x%02x: IOKit 0x%08x", bulkIn, uint32(kr))
	}
	return nil
}

func (d *darwinDevice) Reset() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if kr := C.pm_reset_device(&d.c); kr != 0 {
		return fmt.Errorf("polemaster: device reset: IOKit 0x%08x", uint32(kr))
	}
	return nil
}

func (d *darwinDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	C.pm_close(&d.c)
	return nil
}
