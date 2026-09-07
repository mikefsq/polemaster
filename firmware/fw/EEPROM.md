# Boot EEPROM

The PoleMaster’s boot configuration supplies its USB identity and tells the FX2LP
to wait for firmware from the host. The boot bytes recorded for this camera are:

```text
c0 18 16 40 09 00 00 00
```

| Bytes | Value | Meaning |
|---|---|---|
| 0 | `c0` | C0 boot format: USB identity, followed by a host firmware download |
| 1–2 | `18 16` | Vendor ID `0x1618`, little-endian |
| 3–4 | `40 09` | Product ID `0x0940`, little-endian |
| 5–6 | `00 00` | Device revision `0x0000` |
| 7 | `00` | Configuration byte |

These eight bytes describe the boot configuration, not the EEPROM’s capacity.

## Firmware and recovery

The host downloads firmware into FX2 RAM. Unplugging the camera clears that image;
on reconnect, the camera returns to `1618:0940` and can receive another download.
The running firmware uses `1618:0941`.

This project does not write to the EEPROM. A failed RAM firmware image can normally
be recovered by unplugging and reconnecting the camera. That recovery depends on
an intact boot configuration: custom code that overwrites the EEPROM could prevent
normal USB enumeration and require external repair. The bytes above are a record
of this camera’s configuration, not a general EEPROM restoration procedure.

## Identity response

Vendor request `0xca` in [main.c](main.c) returns a fixed 16-byte array: the eight
bytes above followed by eight zeros. It does **not** read the EEPROM, so the
identity shown by `pmsnap info` cannot verify its current contents.
