#include "fx2.h"
#include "fw.h"

static uint8_t wait_done(void)
{
	uint16_t spins = 20000;

	while (!(I2CS & bmDONE)) {
		if (--spins == 0)
			return 0;
	}
	return 1;
}

static uint8_t i2c_start(uint8_t addr, uint8_t read)
{
	I2CS = bmSTART;
	I2DAT = read ? (addr | 1) : addr;
	if (!wait_done())
		return 0;
	return (I2CS & bmACK) ? 1 : 0;
}

static void wait_stop(void)
{
	uint16_t spins = 20000;

	while (I2CS & bmSTOP) {
		if (--spins == 0)
			return;
	}
}

static void i2c_stop(void)
{
	I2CS = bmSTOP;
	wait_stop();
}

static uint8_t i2c_put(uint8_t b)
{
	I2DAT = b;
	if (!wait_done())
		return 0;
	return (I2CS & bmACK) ? 1 : 0;
}

uint8_t i2c_probe(uint8_t addr)
{
	uint8_t ack = i2c_start(addr, 0);
	i2c_stop();
	return ack;
}

uint8_t sensor_write(uint8_t addr, uint16_t reg, uint16_t val)
{
	uint8_t ok = i2c_start(addr, 0);

	if (ok)
		ok = i2c_put((uint8_t)(reg >> 8));
	if (ok)
		ok = i2c_put((uint8_t)reg);
	if (ok)
		ok = i2c_put((uint8_t)(val >> 8));
	if (ok)
		ok = i2c_put((uint8_t)val);
	i2c_stop();
	return ok;
}

uint8_t sensor_read(uint8_t addr, uint16_t reg, uint8_t *out)
{
	uint8_t dummy;

	if (!i2c_start(addr, 0) || !i2c_put((uint8_t)(reg >> 8)) || !i2c_put((uint8_t)reg)) {
		i2c_stop();
		return 0;
	}
	if (!i2c_start(addr, 1)) {
		i2c_stop();
		return 0;
	}

	// Reading I2DAT starts reception; this first read is not sensor data.
	dummy = I2DAT;
	(void)dummy;
	if (!wait_done()) {
		i2c_stop();
		return 0;
	}
	// Arm the final-byte NAK before reading the first byte starts the next read.
	I2CS = bmLASTRD;
	out[0] = I2DAT;
	if (!wait_done()) {
		i2c_stop();
		return 0;
	}

	// Request STOP before consuming the final byte so no further read starts.
	I2CS = bmSTOP;
	out[1] = I2DAT;
	wait_stop();
	return 1;
}
