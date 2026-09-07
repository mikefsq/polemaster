package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mikefsq/polemaster"
)

var (
	exposure = flag.Duration("exposure", 100*time.Millisecond, "integration time, e.g. 20ms or 2s")
	gain     = flag.Int("gain", 1, "gain step, 1 to 40; 1 to 7 is analog, 1x to 8x")
	offset   = flag.Int("offset", polemaster.OffsetDef, "black level; reaches DATA_PEDESTAL with 50 added")
	out      = flag.String("out", "frame.pgm", "output file for snap")
	frames   = flag.Int("frames", 1, "number of frames to take")
	depth    = flag.Int("depth", 8, "bits per pixel: 8 for the top eight, 12 for all of them")
	roi      = flag.String("roi", "", "readout window as WxH+X+Y, e.g. 640x480+320+240; empty is the whole array")
	fwPath   = flag.String("firmware", "", "load this Intel HEX image instead of the embedded one")
)

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: pmsnap [flags] <list|load|info|snap|regs>\n\n"+
			"  list  report the attached QHY devices and their state\n"+
			"  load  download the camera firmware and open the camera; -firmware loads an\n"+
			"        image of your own instead of the one built in\n"+
			"  info  open the camera and report its identity and every setting in force\n"+
			"  snap  take frames and write them as binary PGM, one byte per pixel at eight\n"+
			"        bits and two at twelve\n"+
			"  regs  dump the sensor register space, or one register given as an argument\n\n"+
			"The camera holds no firmware of its own, so any command that opens it downloads\n"+
			"one first. Unplugging it undoes anything a bad image did.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var err error
	switch flag.Arg(0) {
	case "list":
		err = list()
	case "load":
		err = load()
	case "info":
		err = info()
	case "snap":
		err = snap()
	case "regs":
		err = regs()
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func list() error {
	pids, err := polemaster.Attached()
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		fmt.Println("no QHY device attached")
		return nil
	}
	for _, p := range pids {
		fmt.Printf("%04x:%04x  %s\n", polemaster.VID, p, polemaster.Describe(p))
	}
	return nil
}

func load() error {
	if *fwPath != "" {
		return loadImage(*fwPath)
	}
	d, err := polemaster.Connect()
	if err != nil {
		return err
	}
	defer d.Close()
	fmt.Printf("camera open at %04x:%04x\n", polemaster.VID, d.PID())
	return nil
}

func loadImage(path string) error {
	image, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pids, err := polemaster.Attached()
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		return fmt.Errorf("no QHY device attached")
	}
	d, err := polemaster.Open(pids[0])
	if err != nil {
		return err
	}
	fmt.Printf("downloading %s to %04x:%04x\n", path, polemaster.VID, pids[0])
	err = polemaster.LoadImage(d, string(image))
	d.Close()
	if err != nil {
		return err
	}
	cam, err := polemaster.WaitForCamera(15 * time.Second)
	if err != nil {
		return err
	}
	defer cam.Close()
	fmt.Printf("camera open at %04x:%04x\n", polemaster.VID, cam.PID())
	return nil
}

func open() (*polemaster.Camera, error) {
	c, err := polemaster.OpenCamera()
	if err != nil {
		return nil, err
	}
	if *roi != "" {
		x, y, w, h, err := parseROI(*roi)
		if err != nil {
			c.Close()
			return nil, err
		}
		if err := c.SetROI(x, y, w, h); err != nil {
			c.Close()
			return nil, err
		}
	}
	if err := c.SetDepth(*depth); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.SetOffset(*offset); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.SetGain(*gain); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.SetExposure(*exposure); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func parseROI(spec string) (x, y, w, h int, err error) {
	n, err := fmt.Sscanf(spec, "%dx%d+%d+%d", &w, &h, &x, &y)
	if n < 2 || (err != nil && n < 4) {
		if n2, err2 := fmt.Sscanf(spec, "%dx%d", &w, &h); n2 == 2 && err2 == nil {
			return 0, 0, w, h, nil
		}
		return 0, 0, 0, 0, fmt.Errorf("window %q is not of the form WxH+X+Y", spec)
	}
	return x, y, w, h, nil
}

func info() error {
	c, err := open()
	if err != nil {
		return err
	}
	defer c.Close()

	id, err := c.Identity()
	if err != nil {
		return err
	}
	version, err := c.ReadReg(0x3000)
	if err != nil {
		return err
	}
	y, mo, dy, err := c.FirmwareVersion()
	if err != nil {
		return err
	}
	fmt.Printf("identity     % x\n", id)
	fmt.Printf("firmware     %04d-%02d-%02d\n", y, mo, dy)
	fmt.Printf("sensor       MT9M034 (chip version 0x%04x)\n", version)
	fmt.Printf("frame        %d-bit, %d bytes\n", c.Depth(), c.FrameBytes())
	fmt.Printf("exposure     %s (sensor times up to %s on its own)\n",
		c.Exposure(), polemaster.MaxSensorExposure.Round(time.Millisecond))
	fmt.Printf("gain         %d\n", c.Gain())
	fmt.Printf("offset       %d (DATA_PEDESTAL %d)\n", c.Offset(), c.Offset()+50)
	x, y, w, h := c.ROI()
	fmt.Printf("window       %dx%d+%d+%d\n", w, h, x, y)
	fmt.Printf("readout      %s per frame at any shorter exposure (%.2f fps)\n",
		c.FramePeriod().Round(time.Millisecond), 1/c.FramePeriod().Seconds())
	return nil
}

func snap() error {
	c, err := open()
	if err != nil {
		return err
	}
	defer c.Close()

	fmt.Printf("exposure %s, gain %d, offset %d, %d-bit\n",
		c.Exposure(), c.Gain(), c.Offset(), c.Depth())
	if err := c.Start(); err != nil {
		return err
	}
	defer c.Stop()
	if err := c.Settle(); err != nil {
		return err
	}

	_, _, w, h := c.ROI()
	buf := make([]byte, c.FrameBytes())
	px := make([]uint16, w*h)
	for i := 0; i < *frames; i++ {
		start := time.Now()
		if err := c.Frame(buf); err != nil {
			return err
		}
		if c.Depth() == polemaster.Depth12 {
			polemaster.Pixels12(buf, px)
		} else {
			for j, b := range buf {
				px[j] = uint16(b)
			}
		}
		name := *out
		if *frames > 1 {
			name = fmt.Sprintf("%s.%03d", *out, i)
		}
		if err := writePGM(name, px, w, h, c.Depth()); err != nil {
			return err
		}
		lo, hi, mean := stats(px)
		fmt.Printf("%s  %s  min %d max %d mean %.1f\n",
			name, time.Since(start).Round(time.Millisecond), lo, hi, mean)
	}
	return nil
}

func regs() error {
	d, err := polemaster.Connect()
	if err != nil {
		return err
	}
	defer d.Close()
	c := polemaster.Attach(d)
	if arg := flag.Arg(1); arg != "" {
		reg, err := strconv.ParseUint(arg, 0, 16)
		if err != nil {
			return fmt.Errorf("register %q: %w", arg, err)
		}
		v, err := c.ReadReg(uint16(reg))
		if err != nil {
			return err
		}
		fmt.Printf("0x%04x 0x%04x (%d)\n", reg, v, v)
		return nil
	}
	for reg := 0x3000; reg <= 0x3ffe; reg += 2 {
		v, err := c.ReadReg(uint16(reg))
		if err != nil {
			return fmt.Errorf("read 0x%04x: %w", reg, err)
		}
		if v != 0 {
			fmt.Printf("0x%04x 0x%04x\n", reg, v)
		}
	}
	return nil
}

func writePGM(name string, px []uint16, w, h, depth int) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()

	max := 255
	if depth == polemaster.Depth12 {
		max = 4095
	}
	if _, err := fmt.Fprintf(f, "P5\n%d %d\n%d\n", w, h, max); err != nil {
		return err
	}
	out := make([]byte, 0, len(px)*2)
	for _, v := range px {
		if depth == polemaster.Depth12 {
			out = append(out, byte(v>>8), byte(v))
		} else {
			out = append(out, byte(v))
		}
	}
	_, err = f.Write(out)
	return err
}

func stats(px []uint16) (lo, hi uint16, mean float64) {
	lo, hi = 0xffff, 0
	var sum uint64
	for _, v := range px {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
		sum += uint64(v)
	}
	return lo, hi, float64(sum) / float64(len(px))
}
