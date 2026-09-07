# PoleMaster firmware

Firmware for the QHY PoleMaster’s Cypress FX2LP USB bridge. It configures the pixel
FIFO, forwards sensor register accesses over I2C, and adds a marker to each frame.
The Go driver handles sensor configuration and image capture.

Normal use requires no firmware build: the driver embeds
[`../polemaster-open.hex`](../polemaster-open.hex) and loads it automatically when
the camera is in its boot state. See the [project README](../../README.md) for
camera setup and capture commands.

## Build and try an image

You need SDCC (including `packihx`), Make, and Python 3. Build `pmsnap` first using
the project README, then run these commands from `firmware/fw/`:

```sh
make
../../pmsnap -firmware polemaster-open.hex load
../../pmsnap info
```

The build produces `firmware/fw/polemaster-open.hex`. Loading it replaces the image
in the camera’s RAM; it does not change the image embedded in `pmsnap`. A connected,
responsive camera can accept a replacement in either its boot or running state.

To include the new image in the driver, copy it to the parent directory and rebuild
from the project root:

```sh
cp polemaster-open.hex ..
cd ../..
go build ./cmd/pmsnap
```

`make clean`, run from `firmware/fw/`, removes local build products and leaves the
parent directory’s shipped image intact.

## Recovery and version

If an image leaves the camera unresponsive, unplug and reconnect it. Firmware in
RAM is cleared, and the camera returns to its boot identity, `1618:0940`. See
[EEPROM.md](EEPROM.md) for the limits of this recovery mechanism.

Keep potentially blocking initialization after USB reconnection so a startup
failure does not leave the device disconnected from the bus.

`pmsnap info` decodes the fixed `firmware_version` array in [main.c](main.c) as
`2025-09-06`. This value is not generated from the build time and does not uniquely
identify a build. The USB IDs and strings do not distinguish firmware images either.

## Scope and limitations

The firmware polls the USB control endpoint and uses an interrupt for frame endings.
It requires no fx2lib. It implements the eight vendor requests used by the Go driver
plus an I2C diagnostic request; unsupported requests stall the control endpoint.
See [HARDWARE.md](HARDWARE.md) for the request table and interface details.

Two limitations affect capture:

- A partial pixel packet is overwritten by the frame marker. The driver therefore
  requires a readout window whose pixel count is divisible by 512.
- The added-exposure request uses a busy-wait delay while pixel transfer is disabled.
  It does not stop the sensor clock, so it should not be treated as a precise
  extension of sensor integration time. Control requests also wait for that delay.

## Source files

| File | Purpose |
|---|---|
| [main.c](main.c) | Startup, FIFO setup, vendor requests, and frame-end interrupt |
| [usb.c](usb.c) | USB descriptors and control transfers |
| [i2c.c](i2c.c) | Sensor register access and address probing |
| [fx2.h](fx2.h) | FX2 register definitions |
| [fw.h](fw.h) | Shared function declarations |
| [HARDWARE.md](HARDWARE.md) | Wiring, streaming, and request reference |
| [EEPROM.md](EEPROM.md) | Boot configuration and recovery |
