PREFIX ?= /usr/local/bin

jukebox:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o jukebox ./cmd/jukebox

install: jukebox
	install ./jukebox $(PREFIX)

uninstall: jukebox
	rm $(PREFIX)/jukebox

clean: jukebox
	rm jukebox