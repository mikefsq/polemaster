#ifndef FX2_H
#define FX2_H

#include <stdint.h>

__sfr __at(0x80) IOA;
__sfr __at(0x81) SP;
__sfr __at(0x86) DPS;
__sfr __at(0x87) PCON;
__sfr __at(0x88) TCON;
__sfr __at(0x89) TMOD;
__sfr __at(0x8a) TL0;
__sfr __at(0x8c) TH0;
__sfr __at(0x8e) CKCON;
__sfr __at(0x90) IOB;
__sfr __at(0x91) EXIF;
__sfr __at(0xa0) IOC;
__sfr __at(0xa8) IE;
__sfr __at(0xaa) EP2468STAT;
__sfr __at(0xb0) IOD;
__sfr __at(0xb1) IOE;
__sfr __at(0xb2) OEA;
__sfr __at(0xb3) OEB;
__sfr __at(0xb4) OEC;
__sfr __at(0xb5) OED;
__sfr __at(0xb6) OEE;
__sfr __at(0xb8) IP;
__sfr __at(0xd0) PSW;
__sfr __at(0xe0) ACC;
__sfr __at(0xe8) EIE;
__sfr __at(0xf0) B;

#define bmEA    0x80  // the global enable
#define bmEX0   0x01  // external interrupt 0
#define bmET0   0x02  // timer 0
#define bmIT0   0x01
#define bmIE0   0x02
#define bmTR0   0x10
#define bmTF0   0x20

#define XREG(addr) (*(volatile __xdata unsigned char *)(addr))

#define CPUCS         XREG(0xe600)
#define IFCONFIG      XREG(0xe601)
#define FIFORESET     XREG(0xe604)
#define FIFOPINPOLAR  XREG(0xe609)
#define REVID         XREG(0xe60a)
#define REVCTL        XREG(0xe60b)

#define EP1OUTCFG     XREG(0xe610)
#define EP1INCFG      XREG(0xe611)
#define EP2CFG        XREG(0xe612)
#define EP4CFG        XREG(0xe613)
#define EP6CFG        XREG(0xe614)
#define EP8CFG        XREG(0xe615)
#define EP2FIFOCFG    XREG(0xe618)
#define EP2AUTOINLENH XREG(0xe620)
#define EP2AUTOINLENL XREG(0xe621)
#define INPKTEND      XREG(0xe648)

#define USBIE         XREG(0xe65c)
#define USBIRQ        XREG(0xe65d)
#define PORTACFG      XREG(0xe670)

#define I2CS          XREG(0xe678)
#define I2DAT         XREG(0xe679)
#define I2CTL         XREG(0xe67a)

#define USBCS         XREG(0xe680)
#define FNADDR        XREG(0xe687)
#define EP0BCH        XREG(0xe68a)
#define EP0BCL        XREG(0xe68b)
#define EP2BCH        XREG(0xe690)
#define EP2BCL        XREG(0xe691)
#define EP0CS         XREG(0xe6a0)
#define EP2CS         XREG(0xe6a3)
#define SUDPTRH       XREG(0xe6b3)
#define SUDPTRL       XREG(0xe6b4)
#define SUDPTRCTL     XREG(0xe6b5)

#define SETUPDAT      ((volatile __xdata unsigned char *)0xe6b8)
#define EP0BUF        ((volatile __xdata unsigned char *)0xe740)
#define EP2FIFOBUF    ((volatile __xdata unsigned char *)0xf000)

#define bm8051RES     0x01
#define bmCLKOE       0x02
#define bmCLKINV      0x04
#define bmCLKSPD0     0x08
#define bmCLKSPD1     0x10
#define CPUCS_12MHZ   (bmCLKOE)
#define CPUCS_24MHZ   (bmCLKOE | bmCLKSPD0)
#define CPUCS_48MHZ   (bmCLKOE | bmCLKSPD1)

#define bmDISCON      0x08
#define bmRENUM       0x02
#define bmSIGRSUME    0x01

#define bmSUDAV       0x01
#define bmSOF         0x02
#define bmSUTOK       0x04
#define bmSUSP        0x08
#define bmURES        0x10
#define bmHSGRANT     0x20

#define bmHSNAK       0x80
#define bmEP0BUSY     0x02
#define bmEPSTALL     0x01

#define bmSDPAUTO     0x01

#define bmSTART       0x80
#define bmSTOP        0x40
#define bmLASTRD      0x20
#define bmI2CID       0x18
#define bmBERR        0x04
#define bmACK         0x02
#define bmDONE        0x01

#define bm400KHZ      0x01

// Allow FX2 register writes to cross into the FIFO/interface clock domain.
// Recheck this delay against CPU and IFCLK rates before changing clock settings.
#define SYNCDELAY __asm nop; nop; nop; __endasm

#endif
