jukebox:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o jukebox ./cmd/jukebox
