CC      = sdcc
PACKIHX = packihx

CFLAGS  = -mmcs51 --std-c99 --opt-code-size \
          --code-loc 0x0000 --code-size 0x1b00 \
          --xram-loc 0x1b00 --xram-size 0x0500

SRCS    = main.c usb.c i2c.c
RELS    = $(SRCS:.c=.rel)
TARGET  = polemaster-open

all: $(TARGET).hex

%.rel: %.c fx2.h fw.h
	$(CC) $(CFLAGS) -c $<

$(TARGET).ihx: $(RELS)
	$(CC) $(CFLAGS) -o $@ $(RELS)

$(TARGET).hex: $(TARGET).ihx
	$(PACKIHX) $< > $@
	@python3 -c "import sys; ls=[l.strip() for l in open('$@') if l.startswith(':')]; \
	  d=[l for l in ls if l[7:9]=='00']; \
	  print('--- $@: %d records, %d bytes of code' % (len(d), sum(int(l[1:3],16) for l in d)))"

clean:
	rm -f *.rel *.rst *.sym *.lst *.asm *.ihx *.lk *.map *.mem $(TARGET).hex

.PHONY: all clean
