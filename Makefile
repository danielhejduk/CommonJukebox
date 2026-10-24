PREFIX ?= /usr
BINDIR ?= $(PREFIX)/bin
MANDIR ?= $(PREFIX)/share/man

.PHONY: all jukebox install uninstall test clean

all: jukebox

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)

jukebox:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o jukebox ./cmd/jukebox

install: jukebox
	install -Dm755 jukebox $(DESTDIR)$(BINDIR)/jukebox
	if [ -f jukebox.1 ]; then install -Dm644 jukebox.1 $(DESTDIR)$(MANDIR)/man1/jukebox.1; fi

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/jukebox
	rm -f $(DESTDIR)$(MANDIR)/man1/jukebox.1

test:
	go test -v ./...

clean:
	rm -f jukebox
