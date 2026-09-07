#include "fx2.h"
#include "fw.h"

static uint8_t sensor_addr;

#define REQ_BEGIN_SCAN  0xb3
#define REQ_SENSOR_READ 0xb7
#define REQ_SENSOR_WRITE 0xbb
#define REQ_LONG_EXP    0xc1
#define REQ_FIRMWARE    0xc2
#define REQ_SPEED       0xc8
#define REQ_IDENTITY    0xca
#define REQ_TRANSFER    0xcd
#define REQ_DIAG        0xd0

#define IFCONFIG_IDLE   0x40
#define IFCONFIG_STREAM 0x43

// Fixed protocol date, decoded by the Go driver as 2025-09-06; not a build timestamp.
static const __code uint8_t firmware_version[10] = {
	(9 << 4) | 9, 6, 0, 0, 0, 1, 0, 0x20, 0, 0,
};

// Recorded boot identity padded to 16 bytes; this response does not read EEPROM.
static const __code uint8_t identity[16] = {
	0xc0, 0x18, 0x16, 0x40, 0x09, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
};

static uint32_t added_ms;
static volatile uint8_t frame_pending;
static uint8_t streaming;

// Commit the four-byte marker plus one unspecified byte to match host framing.
// This overwrites any partial pixel packet, hence the driver's ROI restriction.
static void frame_end(void) __interrupt(0)
{
	EP2FIFOBUF[0] = 0xaa;
	EP2FIFOBUF[1] = 0x11;
	EP2FIFOBUF[2] = 0xcc;
	EP2FIFOBUF[3] = 0xee;
	EP2BCH = 0;
	SYNCDELAY;
	EP2BCL = 5;
	frame_pending = 1;
}

static void delay_ms(uint32_t ms)
{
	uint16_t i;

	while (ms--)
		for (i = 0; i < 300; i++)
			__asm nop __endasm;
}

static void endpoints_init(void)
{
	REVCTL = 0x03;
	SYNCDELAY;

	EP1OUTCFG = 0x00;
	SYNCDELAY;
	EP1INCFG = 0x00;
	SYNCDELAY;
	EP4CFG = 0x00;
	SYNCDELAY;
	EP6CFG = 0x00;
	SYNCDELAY;
	EP8CFG = 0x00;
	SYNCDELAY;

	EP2CFG = 0xe8;
	SYNCDELAY;
	EP2FIFOCFG = 0x00;
	SYNCDELAY;

	FIFORESET = 0x80;
	SYNCDELAY;
	FIFORESET = 0x02;
	SYNCDELAY;
	FIFORESET = 0x00;
	SYNCDELAY;

	EP2AUTOINLENH = 0x02;
	SYNCDELAY;
	EP2AUTOINLENL = 0x00;
	SYNCDELAY;
	EP2FIFOCFG = 0x08;
	SYNCDELAY;

	FIFOPINPOLAR = 0x04;
	SYNCDELAY;
	IFCONFIG = IFCONFIG_IDLE;
	SYNCDELAY;
}

static void stream_start(void)
{
	FIFORESET = 0x80;
	SYNCDELAY;
	FIFORESET = 0x02;
	SYNCDELAY;
	FIFORESET = 0x00;
	SYNCDELAY;
	IFCONFIG = IFCONFIG_STREAM;
	SYNCDELAY;
	streaming = 1;
}

static void stream_stop(void)
{
	IFCONFIG = IFCONFIG_IDLE;
	SYNCDELAY;
	streaming = 0;
}

uint8_t vendor_request(void)
{
	uint16_t reg = SETUPDAT[4] | ((uint16_t)SETUPDAT[5] << 8);
	uint8_t n;

	switch (SETUPDAT[1]) {
	case REQ_SENSOR_WRITE:
		n = ep0_recv();
		if (n >= 2)
			sensor_write(sensor_addr, reg,
				     ((uint16_t)EP0BUF[0] << 8) | EP0BUF[1]);
		return 1;

	case REQ_SENSOR_READ:
		if (!sensor_read(sensor_addr, reg, (uint8_t *)EP0BUF)) {
			EP0BUF[0] = 0;
			EP0BUF[1] = 0;
		}
		EP0BCH = 0;
		EP0BCL = 2;
		return 1;

	case REQ_IDENTITY:
		ep0_send(identity, sizeof(identity));
		return 1;

	case REQ_FIRMWARE:
		ep0_send(firmware_version, sizeof(firmware_version));
		return 1;

	case REQ_SPEED:
		n = ep0_recv();
		if (n >= 1) {
			switch (EP0BUF[0]) {
			case 1:
				CPUCS = CPUCS_24MHZ;
				break;
			case 2:
				CPUCS = CPUCS_48MHZ;
				break;
			default:
				CPUCS = CPUCS_12MHZ;
				break;
			}
			SYNCDELAY;
		}
		return 1;

	case REQ_TRANSFER:
		n = ep0_recv();
		if (n >= 1) {
			EP2FIFOCFG = EP0BUF[0] ? 0x09 : 0x08;
			SYNCDELAY;
		}
		return 1;

	case REQ_LONG_EXP:
		n = ep0_recv();
		if (n >= 4)
			added_ms = ((uint32_t)EP0BUF[1] << 16) |
				   ((uint32_t)EP0BUF[2] << 8) | EP0BUF[3];
		return 1;

	case REQ_DIAG: {
		uint8_t a, found = 0;

		EP0BUF[0] = sensor_addr;
		EP0BUF[1] = 0;
		for (a = 2; a < 0xfe && found < 14; a += 2) {
			if (i2c_probe(a))
				EP0BUF[2 + found++] = a;
		}
		EP0BUF[1] = found;
		EP0BCH = 0;
		EP0BCL = 16;
		return 1;
	}

	case REQ_BEGIN_SCAN:
		ep0_recv();
		stream_start();
		return 1;
	}
	return 0;
}

static void sensor_power(void)
{
	OEA |= 0x08;                    // PA3 output
	OEA |= 0x02;                    // PA1 output: the sensor's reset
	OED = 0xf0;                     // PD4 to PD7 outputs
	IOD = 0x00;

	IOA &= ~0x02;                   // hold the sensor in reset
	delay_ms(2);
	IOA |= 0x02;                    // release it
	delay_ms(10);                   // it needs its clock running before it will answer
}

static void find_sensor(void)
{
	sensor_addr = 0x20;
	if (i2c_probe(0x20))
		return;
	if (i2c_probe(0x30))
		sensor_addr = 0x30;
}

void main(void)
{
	// The sensor runs on the FX2's CLKOUT pin
	CPUCS = CPUCS_12MHZ;
	SYNCDELAY;

	// Replace the descriptors
	USBCS |= bmDISCON | bmRENUM;
	SYNCDELAY;

	I2CTL = 0;
	endpoints_init();

	PORTACFG |= 0x01;
	SYNCDELAY;
	TCON |= bmIT0;
	USBIRQ = 0xff;

	delay_ms(20);
	USBCS &= ~bmDISCON;

	sensor_power();
	find_sensor();

	IE |= bmEX0 | bmEA;

	for (;;) {
		if (USBIRQ & bmSUDAV) {
			USBIRQ = bmSUDAV;
			usb_setup();
		}

		if (frame_pending) {
			frame_pending = 0;
			// Gate pixel transfer, not the sensor clock. This busy wait delays USB
			// control handling and is not a precise extension of sensor integration.
			if (added_ms && streaming) {
				IFCONFIG = IFCONFIG_IDLE;
				SYNCDELAY;
				delay_ms(added_ms);
				IFCONFIG = IFCONFIG_STREAM;
				SYNCDELAY;
			}
		}
	}
}
