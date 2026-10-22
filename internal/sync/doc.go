// Package sync keeps the library and a remote alike in both directions. It
// compares each side with a snapshot of the last sync to tell additions,
// changes and deletions apart, and reports files changed on both sides as
// conflicts instead of guessing.
package sync
