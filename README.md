# CommonJukebox
An optionated music library manager. Written in Go.

Features:
- [X] Listing tracks in a music library
- [X] Searching in a music library
- [X] Showing and setting tags
- [X] Checking tags for errors and duplicates
- [X] Adding new music
- [X] Re-encoding the music library
- [X] Syncing with FTP, FTPS, SFTP or a local directory, in both directions

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
