package polemaster

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/mikefsq/polemaster/firmware"
)

func TestFirmwareParses(t *testing.T) {
	recs, err := parseHex(firmware.PoleMaster)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 142 {
		t.Errorf("got %d data records, want 142", len(recs))
	}
	lo, hi, total := 0xffff, 0, 0
	for _, r := range recs {
		if len(r.data) == 0 {
			t.Errorf("record at 0x%04x carries no data", r.addr)
		}
		end := int(r.addr) + len(r.data)
		if end > 0x4000 {
			t.Errorf("record at 0x%04x runs to 0x%04x, past the FX2's internal RAM", r.addr, end)
		}
		if int(r.addr) < lo {
			lo = int(r.addr)
		}
		if end > hi {
			hi = end
		}
		total += len(r.data)
	}
	if lo != 0x0000 || hi != 0x0851 {
		t.Errorf("image covers 0x%04x-0x%04x, want 0x0000-0x0851", lo, hi)
	}
	if total != 2127 {
		t.Errorf("image is %d bytes, want 2127", total)
	}
}

func TestFirmwareRecords(t *testing.T) {
	recs, err := FirmwareRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 142 {
		t.Fatalf("got %d records, want 142", len(recs))
	}
	if recs[0].Addr != 0x0000 || len(recs[0].Data) != 6 {
		t.Errorf("first record is %d bytes at 0x%04x, want 6 at 0x0000",
			len(recs[0].Data), recs[0].Addr)
	}
	last := recs[len(recs)-1]
	if last.Addr != 0x07b5 || len(last.Data) != 4 {
		t.Errorf("last record is %d bytes at 0x%04x, want 4 at 0x07b5", len(last.Data), last.Addr)
	}
}

func TestParseHexRejectsBadChecksum(t *testing.T) {
	if _, err := parseHex(":0400000012345678FF\n:00000001FF\n"); err == nil {
		t.Fatal("a bad checksum was accepted")
	}
	recs, err := parseHex(":0400000012345678\n:00000001FF\n")
	if err != nil {
		t.Fatalf("a record without a checksum was rejected: %v", err)
	}
	if len(recs) != 1 || len(recs[0].data) != 4 {
		t.Fatalf("got %d records, first %d bytes; want one record of 4 bytes", len(recs), len(recs[0].data))
	}
}

func TestParseHexStopsAtEndRecord(t *testing.T) {
	recs, err := parseHex(":0100000041BE\n:00000001FF\n:01000000429D\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1: records past the end marker were read", len(recs))
	}
}

func TestInitTable(t *testing.T) {
	if len(initCmos) != 301 {
		t.Errorf("init table has %d entries, want 301", len(initCmos))
	}
	var delays []time.Duration
	sequencer := 0
	for _, op := range initCmos {
		if op.delay > 0 {
			delays = append(delays, op.delay)
		}
		if op.reg == 0x3086 {
			sequencer++
		}
	}
	if len(delays) != 2 || delays[0] != 100*time.Millisecond || delays[1] != 200*time.Millisecond {
		t.Errorf("waits are %v, want [100ms 200ms]", delays)
	}
	if sequencer != 276 {
		t.Errorf("%d writes to SEQ_DATA_PORT, want 276", sequencer)
	}
	if initCmos[0].reg != 0x30d4 || initCmos[len(initCmos)-1].reg != 0x30ba {
		t.Error("the table does not start at 0x30d4 and end at 0x30ba")
	}
}

func TestRowTiming(t *testing.T) {
	if lineLength != 3150 {
		t.Errorf("LINE_LENGTH_PCK is %d, want 3150: the row length the frame timing follows",
			lineLength)
	}
	full := newCamera(newFake())
	readout := full.FramePeriod()
	if readout < 258*time.Millisecond || readout > 262*time.Millisecond {
		t.Errorf("full-frame readout works out at %s, want about 260ms", readout)
	}
	if MaxSensorExposure < 17*time.Second || MaxSensorExposure > 18*time.Second {
		t.Errorf("longest sensor-timed exposure works out at %s, want about 17s", MaxSensorExposure)
	}
}

func TestROI(t *testing.T) {
	c := newCamera(newFake())
	for _, tc := range []struct {
		w, h  int
		bytes int
	}{
		{Width, Height, 1228800},
		{640, 480, 307200},
		{320, 240, 76800},
	} {
		if err := c.SetROI(0, 0, tc.w, tc.h); err != nil {
			t.Fatalf("%dx%d: %v", tc.w, tc.h, err)
		}
		if got := c.FrameBytes(); got != tc.bytes {
			t.Errorf("%dx%d gives %d bytes, want %d", tc.w, tc.h, got, tc.bytes)
		}
		// The frame runs for the window's rows plus the blanking.
		want := time.Duration(rowTimeUs*float64(tc.h+blanking)) * time.Microsecond
		if c.FramePeriod() != want {
			t.Errorf("%dx%d gives a %s frame, want %s", tc.w, tc.h, c.FramePeriod(), want)
		}
	}
	if err := c.SetROI(0, 0, 640, 480); err != nil {
		t.Fatal(err)
	}
	if x, y, w, h := c.ROI(); x != 0 || y != 0 || w != 640 || h != 480 {
		t.Errorf("window reads back as %dx%d+%d+%d", w, h, x, y)
	}
	for _, bad := range [][4]int{
		{0, 0, 641, 480},   // odd width
		{1, 0, 640, 480},   // odd origin
		{0, 0, 1281, 960},  // wider than the array
		{700, 0, 640, 480}, // runs off the right
		{0, 0, 0, 480},     // empty
	} {
		if err := c.SetROI(bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Errorf("window %dx%d+%d+%d was accepted", bad[2], bad[3], bad[0], bad[1])
		}
	}
	c.streaming = true
	if err := c.SetROI(0, 0, Width, Height); err == nil {
		t.Error("the window was changed during a run")
	}
}

func TestOffsetRegister(t *testing.T) {
	for _, tc := range []struct {
		offset   int
		pedestal uint16
	}{{0, 50}, {150, 200}, {OffsetMax, OffsetMax + offsetBias}} {
		f := newFake()
		c := newCamera(f)
		if err := c.SetOffset(tc.offset); err != nil {
			t.Fatalf("offset %d: %v", tc.offset, err)
		}
		if got := f.regs[regDataPedestal]; got != tc.pedestal {
			t.Errorf("offset %d: DATA_PEDESTAL = %d, want %d", tc.offset, got, tc.pedestal)
		}
		if c.Offset() != tc.offset {
			t.Errorf("offset reads back as %d, want %d", c.Offset(), tc.offset)
		}
	}
	c := newCamera(newFake())
	for _, n := range []int{-1, OffsetMax + 1} {
		if err := c.SetOffset(n); err == nil {
			t.Errorf("offset %d was accepted", n)
		}
	}
}

type fakeDevice struct {
	out   []fakeOut
	regs  map[uint16]uint16
	frame []byte
	pos   int
	limit int
}

type fakeOut struct {
	req      uint8
	val, idx uint16
	data     []byte
}

func newCamera(f *fakeDevice) *Camera {
	return &Camera{d: f, depth: Depth8, roiW: Width, roiH: Height}
}

func newFake() *fakeDevice {
	return &fakeDevice{regs: map[uint16]uint16{regChipVersion: ChipVersionMT9M034}}
}

func (f *fakeDevice) PID() uint16 { return PIDCamera }

func (f *fakeDevice) ControlOut(req uint8, val, idx uint16, data []byte) error {
	f.out = append(f.out, fakeOut{req, val, idx, append([]byte(nil), data...)})
	if req == reqI2CWrite {
		f.regs[idx] = uint16(data[0])<<8 | uint16(data[1])
	}
	return nil
}

func (f *fakeDevice) ControlIn(req uint8, val, idx uint16, data []byte) (int, error) {
	if req == reqI2CRead {
		v := f.regs[idx]
		data[0], data[1] = byte(v>>8), byte(v)
		return 2, nil
	}
	return len(data), nil
}

func (f *fakeDevice) BulkRead(buf []byte, _ time.Duration) (int, error) {
	src := f.frame[f.pos:]
	if f.limit > 0 && f.pos < f.limit && len(src) > f.limit-f.pos {
		src = src[:f.limit-f.pos]
	}
	n := copy(buf, src)
	f.pos += n
	return n, nil
}

func (f *fakeDevice) ClearHalt() error { return nil }
func (f *fakeDevice) Reset() error     { return nil }
func (f *fakeDevice) Close() error     { return nil }

func TestExposureRegisters(t *testing.T) {
	for _, tc := range []struct {
		name    string
		set     time.Duration
		rows    uint16
		extraMs int
	}{
		{"one row", time.Duration(rowTimeUs) * time.Microsecond, 1, 0},
		{"100ms", 100 * time.Millisecond, 380, 0},
		{"at the sensor's limit", MaxSensorExposure, maxRows, 0},
		{"past it", MaxSensorExposure + 4*time.Second, maxRows, 4000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			c := newCamera(f)
			if err := c.SetExposure(tc.set); err != nil {
				t.Fatal(err)
			}
			if got := f.regs[regCoarseIntTime]; got != tc.rows {
				t.Errorf("COARSE_INTEGRATION_TIME = %d rows, want %d", got, tc.rows)
			}
			var long []byte
			for _, o := range f.out {
				if o.req == reqLongExp {
					long = o.data
				}
			}
			if len(long) != 4 {
				t.Fatalf("the bridge got %d bytes of added exposure, want 4", len(long))
			}
			ms := int(long[1])<<16 | int(long[2])<<8 | int(long[3])
			if long[0] != 0 || ms != tc.extraMs {
				t.Errorf("added exposure is % x (%d ms), want %d ms", long, ms, tc.extraMs)
			}
		})
	}
}

func TestSetExposureRejectsNonPositive(t *testing.T) {
	c := newCamera(newFake())
	if err := c.SetExposure(0); err == nil {
		t.Error("a zero exposure was accepted")
	}
}

func TestGainLadder(t *testing.T) {
	for _, tc := range []struct {
		gain    int
		colBits uint16
		dac     uint16
		global  uint16
	}{
		{1, 0x00, 0xd208, 32},
		{2, 0x00, 0xd308, 32},
		{4, 0x10, 0xd308, 32},
		{7, 0x30, 0xd208, 32},
		{8, 0x30, 0xd308, 35},
		{40, 0x30, 0xd308, 255},
	} {
		f := newFake()
		c := newCamera(f)
		c.digitalTest = 0x5330
		if err := c.SetGain(tc.gain); err != nil {
			t.Fatalf("gain %d: %v", tc.gain, err)
		}
		if got := f.regs[regDigitalTest]; got != 0x5300|tc.colBits {
			t.Errorf("gain %d: DIGITAL_TEST = 0x%04x, want 0x%04x", tc.gain, got, 0x5300|tc.colBits)
		}
		if got := f.regs[regDACLD2425]; got != tc.dac {
			t.Errorf("gain %d: DAC_LD_24_25 = 0x%04x, want 0x%04x", tc.gain, got, tc.dac)
		}
		if got := f.regs[regGlobalGain]; got != tc.global {
			t.Errorf("gain %d: GLOBAL_GAIN = %d, want %d", tc.gain, got, tc.global)
		}
	}
}

func TestSetGainRejectsOutOfRange(t *testing.T) {
	c := newCamera(newFake())
	for _, g := range []int{0, GainMax + 1} {
		if err := c.SetGain(g); err == nil {
			t.Errorf("gain %d was accepted", g)
		}
	}
}

func TestFrameCarry(t *testing.T) {
	fill := []byte{0x11, 0x22, 0x33}
	f := newFake()
	for _, b := range fill {
		frame := make([]byte, Width*Height)
		for i := range frame {
			frame[i] = b
		}
		f.frame = append(f.frame, frame...)
		f.frame = append(f.frame, frameMagic[0], frameMagic[1], frameMagic[2], frameMagic[3], 0x07)
	}
	c := newCamera(f)
	buf := make([]byte, c.FrameBytes())
	for i, want := range fill {
		if err := c.Frame(buf); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		for j, b := range buf {
			if b != want {
				t.Fatalf("frame %d byte %d is 0x%02x, want 0x%02x: the frames are not aligned",
					i, j, b, want)
			}
		}
	}
}

func TestFrameTrailerAcrossChunks(t *testing.T) {
	f := newFake()
	f.limit = Width*Height + 2
	for _, b := range []byte{0x11, 0x22} {
		frame := make([]byte, Width*Height)
		for i := range frame {
			frame[i] = b
		}
		f.frame = append(f.frame, frame...)
		f.frame = append(f.frame, frameMagic[0], frameMagic[1], frameMagic[2], frameMagic[3], 0x07)
	}
	c := newCamera(f)
	buf := make([]byte, c.FrameBytes())
	for i, want := range []byte{0x11, 0x22} {
		if err := c.Frame(buf); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		for j, b := range buf {
			if b != want {
				t.Fatalf("frame %d byte %d is 0x%02x, want 0x%02x: the trailer split across two "+
					"reads was not consumed", i, j, b, want)
			}
		}
	}
}

func TestFrameDetectsMisalignment(t *testing.T) {
	f := newFake()
	c := newCamera(f)
	f.frame = make([]byte, 2*c.wireFrame())
	err := c.Frame(make([]byte, c.FrameBytes()))
	if !errors.Is(err, ErrMisaligned) {
		t.Fatalf("got %v, want a misalignment error", err)
	}
}

func TestPixels12(t *testing.T) {
	cases := []struct {
		want  uint16
		frame []byte
	}{
		{0x0123, []byte{0x12, 0xf3}},
		{0x0f0f, []byte{0xf0, 0xff}},
		{0x0abc, []byte{0xab, 0xfc}},
		{0x0555, []byte{0x55, 0xf5}},
		{0x0aaa, []byte{0xaa, 0xfa}},
	}
	for _, tc := range cases {
		out := make([]uint16, 1)
		if n := Pixels12(tc.frame, out); n != 1 {
			t.Fatalf("converted %d pixels, want 1", n)
		}
		if out[0] != tc.want {
			t.Errorf("% x decoded to 0x%04x, want 0x%04x", tc.frame, out[0], tc.want)
		}
	}
}

func TestDepthSizes(t *testing.T) {
	c := newCamera(newFake())
	if got := c.FrameBytes(); got != Width*Height {
		t.Errorf("eight bits gives %d bytes, want %d", got, Width*Height)
	}
	if err := c.SetDepth(Depth12); err != nil {
		t.Fatal(err)
	}
	if got := c.FrameBytes(); got != Width*Height*2 {
		t.Errorf("twelve bits gives %d bytes, want %d", got, Width*Height*2)
	}
	if err := c.SetDepth(10); err == nil {
		t.Error("a depth of 10 was accepted")
	}
	c.streaming = true
	if err := c.SetDepth(Depth8); err == nil {
		t.Error("the depth was changed during a run")
	}
}

func TestFrameRejectsShortBuffer(t *testing.T) {
	c := newCamera(newFake())
	if err := c.Frame(make([]byte, c.FrameBytes()-1)); err == nil {
		t.Error("a buffer smaller than a frame was accepted")
	}
}

func TestFrameRecoversStreamOffset(t *testing.T) {
	for _, depth := range []int{Depth8, Depth12} {
		for _, offset := range []int{17, Width*Height - 2} {
			f := newFake()
			c := newCamera(f)
			if err := c.SetDepth(depth); err != nil {
				t.Fatal(err)
			}
			for _, v := range []byte{1, 2, 3, 4} {
				f.frame = append(f.frame, bytes.Repeat([]byte{v}, c.FrameBytes())...)
				f.frame = append(f.frame, frameMagic[:]...)
				f.frame = append(f.frame, 7)
			}
			f.pos = offset
			buf := make([]byte, c.FrameBytes())
			if err := c.Frame(buf); err != nil {
				t.Fatalf("depth=%d offset=%d: %v", depth, offset, err)
			}
			if !bytes.Equal(buf, bytes.Repeat([]byte{3}, len(buf))) {
				t.Fatalf("depth=%d offset=%d: recovery returned mixed pixels", depth, offset)
			}
			if err := c.Frame(buf); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(buf, bytes.Repeat([]byte{4}, len(buf))) {
				t.Fatal("next frame lost alignment")
			}
		}
	}
}

func TestSettleUsesBufferedMarker(t *testing.T) {
	f := newFake()
	c := newCamera(f)
	c.carry = append([]byte{7, 7}, frameMagic[:]...)
	c.carry = append(c.carry, 9, 1, 2, 3)
	if err := c.settleOnce(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.carry, []byte{1, 2, 3}) || f.pos != 0 {
		t.Fatalf("carry=%v reads=%d", c.carry, f.pos)
	}
}

func TestFrameRecoveryRetriesOnlyOnce(t *testing.T) {
	f := newFake()
	c := newCamera(f)
	f.frame = make([]byte, c.wireFrame())
	f.frame = append(f.frame, frameMagic[:]...)
	f.frame = append(f.frame, 0)
	f.frame = append(f.frame, make([]byte, c.wireFrame())...)
	// A later good frame must not turn a second misalignment into an unbounded retry.
	f.frame = append(f.frame, frameMagic[:]...)
	f.frame = append(f.frame, 0)
	f.frame = append(f.frame, bytes.Repeat([]byte{3}, c.FrameBytes())...)
	f.frame = append(f.frame, frameMagic[:]...)
	f.frame = append(f.frame, 0)
	if err := c.Frame(make([]byte, c.FrameBytes())); !errors.Is(err, ErrMisaligned) {
		t.Fatalf("got %v", err)
	}
}

func TestSettleMarkerCrossesCarryAndUSB(t *testing.T) {
	f := newFake()
	c := newCamera(f)
	c.carry = []byte{7, 0xaa, 0x11}
	f.frame = []byte{0xcc, 0xee, 9, 1, 2, 3}
	if err := c.settleOnce(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.carry, []byte{1, 2, 3}) {
		t.Fatalf("lost marker/read-ahead: %v", c.carry)
	}
}
