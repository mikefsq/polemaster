package polemaster

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	reqI2CWrite  = 0xbb // OUT, wIndex = sensor register, 2 bytes big-endian
	reqI2CRead   = 0xb7 // IN,  wIndex = sensor register, 2 bytes big-endian
	reqIdentity  = 0xca // IN,  wValue 0x10: 16 identity bytes; fixed data in the supplied firmware
	reqLongExp   = 0xc1 // OUT, 4 bytes: the bridge's added exposure, milliseconds
	reqSpeed     = 0xc8 // OUT, 1 byte: link speed index
	reqTransfer  = 0xcd // OUT, 1 byte: 0 for one byte per pixel, 1 for two
	reqBeginScan = 0xb3 // OUT, 1 byte 0x64: release one run of frames
	reqFirmware  = 0xc2 // IN,  10 bytes: encoded version date, fixed in the supplied firmware
)

const (
	regYAddrStart    = 0x3002
	regXAddrStart    = 0x3004
	regYAddrEnd      = 0x3006
	regXAddrEnd      = 0x3008
	regFrameLength   = 0x300a // FRAME_LENGTH_LINES
	regLineLength    = 0x300c // LINE_LENGTH_PCK
	regChipVersion   = 0x3000
	regCoarseIntTime = 0x3012 // COARSE_INTEGRATION_TIME, in rows
	regResetRegister = 0x301a
	regDataPedestal  = 0x301e
	regVTPixClkDiv   = 0x302a
	regVTSysClkDiv   = 0x302c
	regPrePLLClkDiv  = 0x302e
	regPLLMultiplier = 0x3030
	regGreen1Gain    = 0x3056
	regBlueGain      = 0x3058
	regRedGain       = 0x305a
	regGreen2Gain    = 0x305c
	regGlobalGain    = 0x305e
	regSMIATest      = 0x3064
	regOpModeCtrl    = 0x3082
	regDigitalTest   = 0x30b0
	regDACLD2425     = 0x3ee4
)

const ChipVersionMT9M034 = 0x2400

const (
	Width             = 1280
	Height            = 960
	addrOrigin        = 4
	blanking          = 30
	resetRegisterRun  = 0x10dc
	resetRegisterHold = 0x10d8
)

const (
	pllVTPixClkDiv  = 14
	pllVTSysClkDiv  = 1
	pllPrePLLClkDiv = 3
	pllMultiplier   = 0x2a
	pllOpModeCtrl   = 0x29
	pllSMIATest     = 0x1802
	pixelClockMHz   = 12.0
)

const (
	usbTraffic     = 30
	lineLengthBase = 0x672
	lineLengthStep = 50
	lineLength     = lineLengthBase + lineLengthStep*usbTraffic
)

const maxRows = 65000

var (
	rowTimeUs         = float64(lineLength) / pixelClockMHz
	MaxSensorExposure = time.Duration(math.Ceil(rowTimeUs*maxRows)) * time.Microsecond
)

const RowTime = time.Duration(lineLength) * time.Microsecond / pixelClockMHz

const (
	Depth8  = 8
	Depth12 = 12
)

const MaxFrameBytes = Width * Height * 2

const (
	OffsetMin  = 0
	OffsetMax  = 4045
	OffsetDef  = 0
	offsetBias = 50
)

const (
	GainMin = 1
	GainMax = 40
)

type Camera struct {
	d Device

	gain        int
	offset      int
	depth       int
	roiX        int
	roiY        int
	roiW        int
	roiH        int
	exposure    time.Duration
	digitalTest uint16
	streaming   bool

	rx     []byte
	carry  []byte
	settle []byte
	trail  []byte
}

func (c *Camera) Depth() int { return c.depth }

func (c *Camera) FrameBytes() int {
	n := c.roiW * c.roiH
	if c.depth == Depth12 {
		n *= 2
	}
	return n
}

func (c *Camera) ROI() (x, y, w, h int) { return c.roiX, c.roiY, c.roiW, c.roiH }

func (c *Camera) FramePeriod() time.Duration {
	return time.Duration(rowTimeUs*float64(c.roiH+blanking)) * time.Microsecond
}

func (c *Camera) SetROI(x, y, w, h int) error {
	if c.streaming {
		return fmt.Errorf("polemaster: the window cannot be changed while a run is in progress")
	}
	if x|y|w|h < 0 || w == 0 || h == 0 {
		return fmt.Errorf("polemaster: window %dx%d+%d+%d is empty or negative", w, h, x, y)
	}
	if x%2 != 0 || y%2 != 0 || w%2 != 0 || h%2 != 0 {
		return fmt.Errorf("polemaster: window %dx%d+%d+%d must be even in every dimension",
			w, h, x, y)
	}
	if (w*h)%512 != 0 {
		return fmt.Errorf("polemaster: window %dx%d holds %d pixels, which is not a whole "+
			"number of 512-byte packets; the camera would drop the remainder of every frame",
			w, h, w*h)
	}
	if x+w > Width || y+h > Height {
		return fmt.Errorf("polemaster: window %dx%d+%d+%d runs outside the %dx%d array",
			w, h, x, y, Width, Height)
	}

	for _, op := range []regOp{
		{reg: regResetRegister, val: resetRegisterHold},
		{reg: regXAddrStart, val: uint16(addrOrigin + x)},
		{reg: regYAddrStart, val: uint16(addrOrigin + y)},
		{reg: regXAddrEnd, val: uint16(addrOrigin + x + w - 1)},
		{reg: regYAddrEnd, val: uint16(addrOrigin + y + h - 1)},
		{reg: regFrameLength, val: uint16(h + blanking)},
		{reg: regResetRegister, val: resetRegisterRun},
	} {
		if err := c.WriteReg(op.reg, op.val); err != nil {
			return fmt.Errorf("polemaster: set the window at register 0x%04x: %w", op.reg, err)
		}
	}
	c.roiX, c.roiY, c.roiW, c.roiH = x, y, w, h
	c.carry = nil
	return nil
}

func (c *Camera) wireFrame() int { return c.FrameBytes() + frameTrailer }

func (c *Camera) SetDepth(bits int) error {
	var mode byte
	switch bits {
	case Depth8:
		mode = 0
	case Depth12:
		mode = 1
	default:
		return fmt.Errorf("polemaster: depth %d is neither %d nor %d", bits, Depth8, Depth12)
	}
	if c.streaming {
		return fmt.Errorf("polemaster: the depth cannot be changed while a run is in progress")
	}
	if err := c.d.ControlOut(reqTransfer, 0, 0, []byte{mode}); err != nil {
		return fmt.Errorf("polemaster: set the transfer width: %w", err)
	}
	c.depth = bits
	c.carry = nil
	return nil
}

// Pixels12 decodes the camera's wire format: the first byte holds bits 11:4,
// and the second holds bits 3:0 below an unused upper nibble from unconnected inputs.
func Pixels12(frame []byte, out []uint16) int {
	n := len(frame) / 2
	if n > len(out) {
		n = len(out)
	}
	for i := 0; i < n; i++ {
		out[i] = uint16(frame[2*i])<<4 | uint16(frame[2*i+1]&0x0f)
	}
	return n
}

func (c *Camera) WriteReg(reg, val uint16) error {
	return c.d.ControlOut(reqI2CWrite, 0, reg, []byte{byte(val >> 8), byte(val)})
}

func (c *Camera) ReadReg(reg uint16) (uint16, error) {
	buf := make([]byte, 2)
	if _, err := c.d.ControlIn(reqI2CRead, 0, reg, buf); err != nil {
		return 0, err
	}
	return uint16(buf[0])<<8 | uint16(buf[1]), nil
}

func (c *Camera) Identity() ([]byte, error) {
	buf := make([]byte, 16)
	if _, err := c.d.ControlIn(reqIdentity, 0x10, 0, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func Attach(d Device) *Camera { return &Camera{d: d} }

func (c *Camera) FirmwareVersion() (year, month, day int, err error) {
	buf := make([]byte, 10)
	if _, err := c.d.ControlIn(reqFirmware, 0, 0, buf); err != nil {
		return 0, 0, 0, err
	}
	year = int(buf[0] >> 4)
	if year <= 9 {
		year += 0x10
	}
	return 2000 + year, int(buf[0] & 0x0f), int(buf[1]), nil
}

func (c *Camera) Device() Device { return c.d }

func OpenCamera() (*Camera, error) {
	d, err := Connect()
	if err != nil {
		return nil, err
	}
	c := &Camera{d: d}
	if err := c.Init(); err != nil {
		d.Close()
		return nil, err
	}
	return c, nil
}

func (c *Camera) Close() error {
	if c.streaming {
		c.Stop()
	}
	return c.d.Close()
}

func (c *Camera) Init() error {
	got, err := c.ReadReg(regChipVersion)
	if err != nil {
		return fmt.Errorf("polemaster: read the sensor chip version: %w", err)
	}
	if got != ChipVersionMT9M034 {
		return fmt.Errorf("polemaster: sensor reports chip version 0x%04x, expected 0x%04x (MT9M034)",
			got, ChipVersionMT9M034)
	}

	if err := c.d.ControlOut(reqSpeed, 0, 0, []byte{0}); err != nil {
		return fmt.Errorf("polemaster: set the link speed: %w", err)
	}
	for _, op := range initCmos {
		if err := c.WriteReg(op.reg, op.val); err != nil {
			return fmt.Errorf("polemaster: sensor init at register 0x%04x: %w", op.reg, err)
		}
		if op.delay > 0 {
			time.Sleep(op.delay)
		}
	}

	c.digitalTest = 0x5330
	if err := c.WriteReg(regDigitalTest, c.digitalTest); err != nil {
		return fmt.Errorf("polemaster: readout setup at register 0x%04x: %w", regDigitalTest, err)
	}
	for _, op := range []regOp{
		{reg: regResetRegister, val: resetRegisterRun},
		{reg: regLineLength, val: lineLength},
		{reg: regVTPixClkDiv, val: pllVTPixClkDiv},
		{reg: regVTSysClkDiv, val: pllVTSysClkDiv},
		{reg: regPrePLLClkDiv, val: pllPrePLLClkDiv},
		{reg: regPLLMultiplier, val: pllMultiplier},
		{reg: regOpModeCtrl, val: pllOpModeCtrl},
		{reg: regSMIATest, val: pllSMIATest},
		{reg: regBlueGain, val: 0},
		{reg: regRedGain, val: 0},
		{reg: regGreen1Gain, val: 0},
		{reg: regGreen2Gain, val: 0},
	} {
		if err := c.WriteReg(op.reg, op.val); err != nil {
			return fmt.Errorf("polemaster: readout setup at register 0x%04x: %w", op.reg, err)
		}
	}

	if err := c.SetROI(0, 0, Width, Height); err != nil {
		return err
	}
	if err := c.SetDepth(Depth8); err != nil {
		return err
	}
	if err := c.SetOffset(OffsetDef); err != nil {
		return err
	}
	if err := c.SetGain(1); err != nil {
		return err
	}
	return c.SetExposure(100 * time.Millisecond)
}

var analogGain = []struct {
	colBits uint16
	dac     uint16
}{
	1: {0x00, 0xd208},
	2: {0x00, 0xd308},
	3: {0x10, 0xd208},
	4: {0x10, 0xd308},
	5: {0x20, 0xd208},
	6: {0x20, 0xd308},
	7: {0x30, 0xd208},
}

func (c *Camera) SetGain(g int) error {
	if g < GainMin || g > GainMax {
		return fmt.Errorf("polemaster: gain %d out of range %d-%d", g, GainMin, GainMax)
	}
	colBits, dac := uint16(0x30), uint16(0xd308)
	digital := 32.0
	if g <= 7 {
		colBits, dac = analogGain[g].colBits, analogGain[g].dac
	} else {
		for i := 0; i < g-7; i++ {
			digital *= 1.1
		}
		if digital > 255 {
			digital = 255
		}
	}
	c.digitalTest = c.digitalTest&^0x30 | colBits
	for _, op := range []regOp{
		{reg: regDigitalTest, val: c.digitalTest},
		{reg: regDACLD2425, val: dac},
		{reg: regGlobalGain, val: uint16(digital)},
	} {
		if err := c.WriteReg(op.reg, op.val); err != nil {
			return fmt.Errorf("polemaster: set gain at register 0x%04x: %w", op.reg, err)
		}
	}
	c.gain = g
	return nil
}

func (c *Camera) Gain() int { return c.gain }

func (c *Camera) SetOffset(n int) error {
	if n < OffsetMin || n > OffsetMax {
		return fmt.Errorf("polemaster: offset %d out of range %d-%d", n, OffsetMin, OffsetMax)
	}
	if err := c.WriteReg(regDataPedestal, uint16(n+offsetBias)); err != nil {
		return fmt.Errorf("polemaster: set the offset: %w", err)
	}
	c.offset = n
	return nil
}

func (c *Camera) Offset() int { return c.offset }

func (c *Camera) SetExposure(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("polemaster: exposure %s is not positive", d)
	}
	us := float64(d.Microseconds())
	rows, extraMs := maxRows, 0
	if us <= rowTimeUs*maxRows {
		rows = int(us / rowTimeUs)
		if rows < 1 {
			rows = 1
		}
	} else {
		extraMs = int((us - rowTimeUs*maxRows) / 1000)
	}

	if err := c.d.ControlOut(reqLongExp, 0, 0, []byte{
		0, byte(extraMs >> 16), byte(extraMs >> 8), byte(extraMs),
	}); err != nil {
		return fmt.Errorf("polemaster: set the added exposure: %w", err)
	}
	if err := c.WriteReg(regCoarseIntTime, uint16(rows)); err != nil {
		return fmt.Errorf("polemaster: set the integration rows: %w", err)
	}
	c.exposure = time.Duration(float64(rows)*rowTimeUs)*time.Microsecond +
		time.Duration(extraMs)*time.Millisecond
	return nil
}

func (c *Camera) Exposure() time.Duration { return c.exposure }

func (c *Camera) Start() error {
	c.carry = nil
	if err := c.d.ClearHalt(); err != nil {
		return err
	}
	if err := c.d.ControlOut(reqBeginScan, 0, 0, []byte{0x64}); err != nil {
		return fmt.Errorf("polemaster: begin the scan: %w", err)
	}
	c.streaming = true
	return nil
}

// Stop clears host capture state and the endpoint. The firmware has no stop
// request, so the sensor continues reading out.
func (c *Camera) Stop() error {
	c.streaming = false
	c.carry = nil
	return c.d.ClearHalt()
}

const settleChunk = 64 << 10

// The firmware's frame_end handler commits four marker bytes and one unspecified
// byte. Consume all five, but validate only the marker.
const frameTrailer = 5

var frameMagic = [4]byte{0xaa, 0x11, 0xcc, 0xee}

const rxChunk = MaxFrameBytes + frameTrailer

var ErrMisaligned = errors.New("polemaster: the frame stream has slipped out of alignment")

func (c *Camera) Frame(buf []byte) error {
	if len(buf) < c.FrameBytes() {
		return fmt.Errorf("polemaster: frame buffer holds %d bytes, need %d",
			len(buf), c.FrameBytes())
	}
	if c.rx == nil {
		c.rx = make([]byte, rxChunk)
	}

	c.trail = c.trail[:0]
	got := c.take(buf, 0, c.carry)
	c.carry = c.carry[got:]

	deadline := time.Now().Add(2*c.exposure + 5*time.Second)
	for got < c.wireFrame() {
		n, err := c.d.BulkRead(c.rx[:c.wireFrame()], time.Until(deadline))
		if err != nil {
			c.carry = nil
			return err
		}
		if n == 0 {
			c.carry = nil
			return fmt.Errorf("polemaster: frame read stalled after %d of %d bytes",
				got, c.wireFrame())
		}
		used := c.take(buf, got, c.rx[:n])
		got += used
		if used < n {
			c.carry = append(c.carry[:0:0], c.rx[used:n]...)
		}
	}
	if len(c.trail) < len(frameMagic) || [4]byte(c.trail[:4]) != frameMagic {
		c.carry = nil
		return fmt.Errorf("%w: the frame ended with % x, expected it to start with % x",
			ErrMisaligned, c.trail, frameMagic[:])
	}
	return nil
}

func (c *Camera) take(buf []byte, got int, src []byte) int {
	room := c.wireFrame() - got
	if room > len(src) {
		room = len(src)
	}
	pixels := c.FrameBytes() - got
	if pixels > room {
		pixels = room
	}
	if pixels > 0 {
		copy(buf[got:got+pixels], src[:pixels])
	} else {
		pixels = 0
	}
	c.trail = append(c.trail, src[pixels:room]...)
	return room
}

// Settle finds a frame boundary, then discards one complete frame: that frame
// may have begun integrating before the current settings took effect. Call after Start.
func (c *Camera) Settle() error {
	if err := c.settle1(); err != nil {
		return err
	}

	if c.settle == nil {
		c.settle = make([]byte, MaxFrameBytes)
	}
	return c.Frame(c.settle)
}

func (c *Camera) settle1() error {
	err := c.settleOnce()
	if err == nil || !errors.Is(err, ErrMisaligned) {
		return err
	}

	// A previous interrupted run can leave the endpoint stalled; re-arm once
	// if no frame boundary appeared during the settling window.
	if err := c.Start(); err != nil {
		return err
	}
	return c.settleOnce()
}

func (c *Camera) settleOnce() error {
	if c.rx == nil {
		c.rx = make([]byte, rxChunk)
	}

	deadline := time.Now().Add(2*(c.exposure+c.FramePeriod()) + 5*time.Second)

	scan := make([]byte, 0, rxChunk+frameTrailer)
	seen := 0
	for {
		n, err := c.d.BulkRead(c.rx[:settleChunk], time.Until(deadline))
		if err != nil {
			c.carry = nil
			return err
		}
		seen += n
		scan = append(scan, c.rx[:n]...)
		for i := 0; i+frameTrailer <= len(scan); i++ {
			if [4]byte(scan[i:i+4]) == frameMagic {
				c.carry = append(c.carry[:0:0], scan[i+frameTrailer:]...)
				return nil
			}
		}
		if len(scan) > frameTrailer-1 {
			scan = append(scan[:0], scan[len(scan)-(frameTrailer-1):]...)
		}
		if time.Now().After(deadline) {
			c.carry = nil
			return fmt.Errorf("%w: no frame marker in the %d bytes that arrived while settling",
				ErrMisaligned, seen)
		}
	}
}

func (c *Camera) Snap() ([]byte, error) {
	if err := c.Start(); err != nil {
		return nil, err
	}
	defer c.Stop()
	if err := c.Settle(); err != nil {
		return nil, err
	}
	buf := make([]byte, c.FrameBytes())
	if err := c.Frame(buf); err != nil {
		return nil, err
	}
	return buf, nil
}
