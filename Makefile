PREFIX ?= /usr
BINDIR ?= $(PREFIX)/bin
MANDIR ?= $(PREFIX)/share/man

.PHONY: all jukebox install uninstall clean

all: jukebox

jukebox:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o jukebox ./cmd/jukebox

install: jukebox
	install -Dm755 jukebox $(DESTDIR)$(BINDIR)/jukebox
	install -Dm644 jukebox.1 $(DESTDIR)$(MANDIR)/man1/jukebox.1

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/jukebox
	rm -f $(DESTDIR)$(MANDIR)/man1/jukebox.1

clean:
	rm -f jukebox
