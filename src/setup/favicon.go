package setup

import (
	_ "embed"
	"net/http"
	"strconv"
)

//go:embed favicon.ico
var faviconICO []byte

func FaviconHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Content-Length", strconv.Itoa(len(faviconICO)))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodHead {
			_, _ = w.Write(faviconICO)
		}
	})
}
