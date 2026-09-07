#ifndef FW_H
#define FW_H

#include <stdint.h>

void usb_setup(void);
void ep0_send(const __code uint8_t *src, uint16_t len);
uint8_t ep0_recv(void);

uint8_t vendor_request(void);

uint8_t i2c_probe(uint8_t addr);
uint8_t sensor_write(uint8_t addr, uint16_t reg, uint16_t val);
uint8_t sensor_read(uint8_t addr, uint16_t reg, uint8_t *out);

#endif
