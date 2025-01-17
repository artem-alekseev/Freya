TOPDIR:=$(shell pwd)
BINDIR:=$(TOPDIR)/bin
SRVDIR:=$(TOPDIR)/cmd

TARGETS:=loginserver gameserver masterserver

.PHONY: pre all clean

all: pre
	$(foreach server,$(TARGETS), \
		go build -v -o $(BINDIR)/$(server) $(SRVDIR)/$(server);)

pre:
	mkdir -p $(BINDIR)

clean:
	rm -r $(BINDIR)

%:
	$(MAKE) -C $(TOPDIR) pre
	go build -v -o $(BINDIR)/$@ $(SRVDIR)/$@