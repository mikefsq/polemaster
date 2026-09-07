#include "fx2.h"
#include "fw.h"

/* Descriptor types. */
#define DSCR_DEVICE     1
#define DSCR_CONFIG     2
#define DSCR_STRING     3
#define DSCR_QUALIFIER  6
#define DSCR_OTHERSPEED 7

static const __code uint8_t dscr_device[18] = {
	18, DSCR_DEVICE,
	0x00, 0x02,             /* USB 2.00 */
	0xff, 0xff, 0xff,       /* vendor specific class, subclass and protocol */
	64,                     /* endpoint zero's packet size */
	0x18, 0x16,             /* idVendor  0x1618 */
	0x41, 0x09,             /* idProduct 0x0941 */
	0x00, 0x00,             /* bcdDevice */
	1, 2, 0,                /* manufacturer, product, no serial */
	1,                      /* one configuration */
};

static const __code uint8_t dscr_qualifier[10] = {
	10, DSCR_QUALIFIER,
	0x00, 0x02,
	0xff, 0xff, 0xff,
	64,
	1,                      /* one configuration at the other speed */
	0,
};

#define CONFIG_LEN 25
#define CONFIG_BODY(type, pktlo, pkthi)                              \
	9, (type),                                                       \
	CONFIG_LEN, 0,                                                   \
	1,                      /* one interface */                      \
	1,                      /* configuration value */                \
	0,                      /* no string */                          \
	0x80,                   /* bus powered, no remote wakeup */      \
	250,                    /* 500 mA */                             \
                                                                     \
	9, 4,                   /* interface */                          \
	0, 0,                   /* interface zero, alternate setting zero */ \
	1,                      /* one endpoint */                       \
	0xff, 0xff, 0xff,       /* vendor specific */                    \
	0,                                                               \
                                                                     \
	7, 5,                   /* endpoint */                           \
	0x82,                   /* IN, endpoint 2 */                     \
	0x02,                   /* bulk */                               \
	(pktlo), (pkthi),                                                \
	0                       /* no interval for bulk */

static const __code uint8_t dscr_config_hs[CONFIG_LEN] = {
	CONFIG_BODY(DSCR_CONFIG, 0x00, 0x02),
};

static const __code uint8_t dscr_config_fs[CONFIG_LEN] = {
	CONFIG_BODY(DSCR_OTHERSPEED, 64, 0),
};

/* Strings, in the USB's UTF-16LE. */
static const __code uint8_t str_lang[4] = {4, DSCR_STRING, 0x09, 0x04};
static const __code uint8_t str_vendor[] = {
	14, DSCR_STRING, 'Q', 0, 'H', 0, 'Y', 0, 'C', 0, 'C', 0, 'D', 0,
};
static const __code uint8_t str_product[] = {
	30, DSCR_STRING,
	'Q', 0, 'H', 0, 'Y', 0, '-', 0, 'P', 0, 'o', 0, 'l', 0, 'e', 0,
	'M', 0, 'a', 0, 's', 0, 't', 0, 'e', 0, 'r', 0,
};

static uint8_t config_value;

void ep0_send(const __code uint8_t *src, uint16_t len)
{
	uint16_t want = SETUPDAT[6] | ((uint16_t)SETUPDAT[7] << 8);
	uint8_t i;

	if (len > want)
		len = want;
	for (i = 0; i < (uint8_t)len; i++)
		EP0BUF[i] = src[i];
	EP0BCH = 0;
	EP0BCL = (uint8_t)len;
}

uint8_t ep0_recv(void)
{
	EP0BCH = 0;
	EP0BCL = 0;
	while (EP0CS & bmEP0BUSY)
		;
	return EP0BCL;
}

static uint8_t usb_standard(void)
{
	uint8_t req = SETUPDAT[1];
	uint8_t type = SETUPDAT[3];

	switch (req) {
	case 0x06:
		switch (type) {
		case DSCR_DEVICE:
			ep0_send(dscr_device, sizeof(dscr_device));
			return 1;
		case DSCR_QUALIFIER:
			ep0_send(dscr_qualifier, sizeof(dscr_qualifier));
			return 1;
		case DSCR_CONFIG:
			ep0_send(dscr_config_hs, CONFIG_LEN);
			return 1;
		case DSCR_OTHERSPEED:
			ep0_send(dscr_config_fs, CONFIG_LEN);
			return 1;
		case DSCR_STRING:
			switch (SETUPDAT[2]) {
			case 0:
				ep0_send(str_lang, sizeof(str_lang));
				return 1;
			case 1:
				ep0_send(str_vendor, sizeof(str_vendor));
				return 1;
			case 2:
				ep0_send(str_product, sizeof(str_product));
				return 1;
			}
			return 0;
		}
		return 0;

	case 0x08:
		EP0BUF[0] = config_value;
		EP0BCH = 0;
		EP0BCL = 1;
		return 1;

	case 0x09:
		config_value = SETUPDAT[2];
		return 1;

	case 0x0a:
		EP0BUF[0] = 0;
		EP0BCH = 0;
		EP0BCL = 1;
		return 1;

	case 0x0b:
		return 1;

	case 0x00:
		EP0BUF[0] = 0;
		EP0BUF[1] = 0;
		EP0BCH = 0;
		EP0BCL = 2;
		return 1;

	case 0x01:
	case 0x03:
		return 1;
	}
	return 0;
}

void usb_setup(void)
{
	uint8_t handled;

	if ((SETUPDAT[0] & 0x60) == 0x40)
		handled = vendor_request();
	else
		handled = usb_standard();

	if (handled)
		EP0CS |= bmHSNAK;
	else
		EP0CS |= bmEPSTALL;
}
