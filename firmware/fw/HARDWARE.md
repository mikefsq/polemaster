# Hardware and USB interface

The PoleMaster connects an MT9M034 monochrome sensor directly to the FX2LP’s slave
FIFO interface, without an FPGA. Pixels flow into the USB endpoint in hardware;
the processor handles control requests, sensor I2C access, and frame markers.

These notes describe the wiring used by this project and the implementation in
[main.c](main.c). See [README.md](README.md) for building and loading firmware.

## Signals and startup

| Signal | FX2 connection |
|---|---|
| Sensor pixel clock | IFCLK, driven by the sensor |
| Pixel data | FD[7:0], or FD[15:0] for two-byte transfers |
| Data valid | SLWR, active high |
| Frame-valid falling edge | PA0 / INT0 |
| Clock supplied to sensor | CLKOUT |
| Sensor reset | PA1 |
| Sensor control | SCL and SDA on the FX2 I2C controller |

The sensor needs its clock and reset released before it answers I2C requests.
`CPUCS` selects the processor clock and enables CLKOUT; preserve `CLKOE` when
changing it. The default is 12 MHz. Changing the clock also affects sensor timing
and invalidates the Go driver’s fixed exposure calculations.

Startup configures PA1 and PA3 as outputs, sets PD4–PD7 as outputs, drives port D
low, then holds PA1 low for a delay before releasing it. The firmware probes sensor
addresses `0x20` and `0x30`. These are eight-bit I2C address bytes, corresponding to
seven-bit addresses `0x10` and `0x18`.

## Pixel transfer

| Register | Value | Purpose |
|---|---|---|
| `IFCONFIG` | `0x40` idle, `0x43` streaming | Switch between ports and externally clocked slave FIFO mode |
| `EP2CFG` | `0xe8` | Enable bulk IN endpoint 2 with quad buffering |
| `EP2FIFOCFG` | `0x08` or `0x09` | AUTOIN, with optional 16-bit transfer width |
| `EP2AUTOINLEN` | `0x0200` | Commit each 512-byte pixel packet |
| `FIFOPINPOLAR` | `0x04` | Make SLWR active high |
| `PORTACFG` | bit 0 set | Route PA0 to INT0 |

The host reads endpoint `0x82`. Starting a run resets the FIFO and switches
`IFCONFIG` to `0x43`. Switching back to `0x40` stops pixels entering the FIFO; it
does not stop sensor readout. There is no vendor stop command in this firmware.

## Frame boundaries and window sizes

The falling edge of frame-valid triggers INT0. Its handler writes `aa 11 cc ee`
at the start of the endpoint buffer and commits five bytes. The fifth byte is
unspecified because the handler does not write it. The host checks the four-byte
marker to identify a frame boundary; a short USB packet alone is insufficient.

Set `IT0` in `TCON` for edge-triggered operation and `EX0` in `IE` to enable the
interrupt. Both masks are `0x01`, but they belong to different registers.
Level-triggered operation can repeatedly emit markers while the signal stays low.

Because the handler overwrites the start of the buffer, any partial pixel packet
is lost. The driver requires the window’s pixel count to be divisible by 512 at
both depths. Valid examples include 320×240, 640×480, 960×720, and 1280×960. An
800×600 window is rejected.

Window coordinates and dimensions must also be even and fit within the sensor.
`Camera.SetROI` rejects changes during a capture run. Before capture, it holds
sensor readout with `RESET_REGISTER = 0x10d8`, writes the geometry, and resumes
readout with `0x10dc`.

## Vendor requests

Directions are relative to the host. This table describes the current firmware,
including its diagnostic extension; it is not a complete QHY SDK protocol reference.

| Request | Direction | Payload and behavior |
|---|---|---|
| `0xb3` | OUT | Start a run; the driver sends one byte, `0x64` |
| `0xb7` | IN | Read sensor register selected by `wIndex`; returns two bytes, big-endian |
| `0xbb` | OUT | Write sensor register selected by `wIndex`; two bytes, big-endian |
| `0xc1` | OUT | Four bytes; bytes 1–3 encode added delay in milliseconds, big-endian; byte 0 is ignored |
| `0xc2` | IN | Return the fixed 10-byte firmware version array |
| `0xc8` | OUT | One byte: `1` selects 24 MHz, `2` selects 48 MHz, other values select 12 MHz; CLKOUT stays enabled |
| `0xca` | IN | Return a fixed 16-byte identity array; the driver sends `wValue = 0x10`, which this firmware ignores |
| `0xcd` | OUT | One byte: zero selects one byte per pixel, nonzero selects two |
| `0xd0` | IN | Return 16 diagnostic bytes: selected sensor address, responding address count, then up to 14 eight-bit I2C addresses |

For `0xd0`, entries beyond the returned address count are unspecified. The Go
driver does not use this request. A failed sensor read returns two zero bytes;
sensor write failures are not reported to the host. Other vendor requests stall
the control endpoint.
