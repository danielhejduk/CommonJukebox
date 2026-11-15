# CommonJukebox
An opinionated music library manager for FLAC, WAV, Ogg Vorbis and Opus files.
Written in Go.

`init` -> `add` -> `check` -> `sync`

Features:
- [X] Listing tracks in a music library
- [X] Searching in a music library
- [X] Showing and setting tags
- [X] Embedding and extracting cover art
- [X] Checking tags for errors and duplicates
- [X] Adding new music
- [X] Re-encoding the music library
- [X] Syncing with FTP, FTPS, SFTP or a local directory, in both directions

## Installing

Needs Go 1.26 and, for adding and re-encoding music, `ffmpeg`.

On macOS, Homebrew's `ffmpeg` is built without the Vorbis encoder (libvorbis).
For a `vorbis` library, install `ffmpeg-full` instead and put it on your `PATH`:

```sh
brew install ffmpeg-full
export PATH="$(brew --prefix ffmpeg-full)/bin:$PATH"
```

```sh
make
sudo make install    # installs to /usr; set PREFIX to change that
```

## Quick start

```sh
jukebox init --codec opus ~/Music       # pick the library and its codec
jukebox add ~/Downloads/album           # check and copy new tracks in
jukebox search paranoid android
jukebox check                           # find missing or inconsistent tags
jukebox tag set --year 1997 ~/Music/Radiohead/OK\ Computer
jukebox tag art set cover.jpg ~/Music/Radiohead/OK\ Computer
```

## Syncing

```sh
jukebox remote add phone ftps://me@192.168.1.20:2121/Music
jukebox sync --dry-run phone
jukebox sync phone
```

SFTP (`sftp://me@host/Music`) logs in with your SSH agent or keys and needs
the host in `~/.ssh/known_hosts`. Otherwise the password comes from `JUKEBOX_PASSWORD`, `~/.netrc` or a prompt. A track
changed on both sides is reported and skipped; settle it with
`--prefer local` or `--prefer remote`. See `man jukebox` for details.
