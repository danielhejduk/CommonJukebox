package gonic

import (
	"net/http"
	"time"

	"github.com/supersonic-app/go-subsonic/subsonic"
)

func Open(rawURL, password string) {
	client := subsonic.Client{Client: &http.Client{Timeout: 30 * time.Second}, BaseUrl: scheme+host, User: u.User.Username(), ClientName: "commonjukebox", UseJSON: true}
}
