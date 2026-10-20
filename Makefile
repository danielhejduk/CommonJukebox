PREFIX ?= /usr

jukebox:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o jukebox ./cmd/jukebox

install: jukebox
	install ./jukebox $(PREFIX)/bin
	install ./jukebox.1 $(PREFIX)/share/man1

uninstall: jukebox
	rm $(PREFIX)/bin/jukebox
	rm $(PREFIX)/share/man1/jukebox.1

clean: jukebox
	rm jukebox