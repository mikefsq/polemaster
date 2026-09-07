# polemaster

A Go driver and command-line capture tool for the QHY PoleMaster USB camera on
macOS and Linux. Capture 1280×960 monochrome images at 8 or 12 bits, with control
over exposure, gain, black level, and readout window.

The project includes its own FX2LP firmware and talks directly to the camera over
USB, without QHY’s SDK. It provides image capture and a Go API; it does not include
a graphical interface or a polar-alignment workflow.

## Build

You need Go 1.23 or later and a QHY PoleMaster camera to capture images. Run the
build command from the project directory.

**macOS:** Install Apple’s Command Line Tools (`xcode-select --install`) if needed.
The build uses cgo and the system IOKit and CoreFoundation frameworks.

```sh
go build ./cmd/pmsnap
```

**Linux:** The driver uses usbfs and builds without cgo.

```sh
CGO_ENABLED=0 go build ./cmd/pmsnap
```

Your user needs read and write access to the camera’s USB device. One option is to
save these rules in `/etc/udev/rules.d/99-polemaster.rules`; they allow all local
users to access the PoleMaster in both its boot and running states:

```udev
SUBSYSTEM=="usb", ATTR{idVendor}=="1618", ATTR{idProduct}=="0940", MODE="0666"
SUBSYSTEM=="usb", ATTR{idVendor}=="1618", ATTR{idProduct}=="0941", MODE="0666"
```

Reload the rules with `sudo udevadm control --reload-rules`, then unplug and
reconnect the camera.

If a USB frame loses alignment, the driver scans for the next frame boundary
and retries once. It validates the replacement frame before returning it;
unrecoverable alignment failures return `ErrMisaligned`.

## Capture an image

Connect the camera, then run:

```sh
./pmsnap list
./pmsnap -exposure 200ms -gain 5 snap
```

This writes `frame.pgm` in the current directory and prints the image’s minimum,
maximum, and mean pixel values. Open the file in an image viewer that supports
binary PGM. Existing output files are overwritten.

Firmware loads automatically when the camera is in its boot state. It lives in
RAM and is lost when the camera is unplugged; no separate firmware installation
or build is needed.

Put flags **before** the command:

```sh
./pmsnap info                                  # camera and current settings
./pmsnap -depth 12 -exposure 2s snap            # preserve all 12 sensor bits
./pmsnap -frames 10 -out seq.pgm snap           # seq.pgm.000 through seq.pgm.009
./pmsnap -roi 640x480+320+240 snap              # read a central window
./pmsnap -h                                   # commands and options
```

| Option | Default | Meaning |
|---|---|---|
| `-exposure` | `100ms` | Integration time, such as `20ms` or `2s` |
| `-gain` | `1` | Gain step, 1–40; steps 1–7 use analog gain, higher steps add digital gain |
| `-offset` | `0` | Black-level adjustment, 0–4045; the sensor pedestal is this value plus 50 |
| `-depth` | `8` | Pixel depth: `8` or `12` |
| `-roi` | full frame | Readout window as `WxH+X+Y`, or `WxH` starting at the corner |
| `-frames` | `1` | Number of images to capture |
| `-out` | `frame.pgm` | Output filename; multiple images append `.000`, `.001`, etc. |
| `-firmware` | embedded | Custom Intel HEX image, used with `load` |

Eight-bit PGM files contain one byte per pixel, with values from 0 to 255.
Twelve-bit files contain two bytes per pixel, with values from 0 to 4095.

## Capture notes

- Full-frame readout takes about 260 ms, limiting short exposures to roughly
  3.8 frames per second. A shorter readout window allows faster capture.
- Window coordinates and dimensions must be even, fit within 1280×960, and have
  a pixel count divisible by 512. Examples include 640×480 and 320×240.
- The driver discards initial frames to let settings take effect. The first image
  can take several exposure periods to arrive.
- If the camera stops responding, unplug and reconnect it, then retry. On Linux,
  also check USB permissions if the camera cannot be opened.

## Development

The Go package exposes `OpenCamera`, settings methods, and `Camera.Snap` for
single-frame capture. Close the camera when finished. `Snap` returns raw pixel
bytes; use `polemaster.Pixels12` to decode 12-bit data. For a capture loop and PGM
output, see [the pmsnap source](cmd/pmsnap/main.go).

Run the unit tests, which use a simulated USB device:

```sh
go test ./...
```

For sensor diagnostics or custom firmware:

```sh
./pmsnap regs                                 # dump nonzero sensor registers
./pmsnap regs 0x300c                           # read one register
./pmsnap -firmware other.hex load              # load a custom firmware image
```

See the [firmware build instructions](firmware/fw/README.md),
[hardware notes](firmware/fw/HARDWARE.md), and
[EEPROM notes](firmware/fw/EEPROM.md) for implementation details.

## Licence

[MIT](LICENSE).
