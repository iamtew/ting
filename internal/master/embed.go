package master

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
var indexPage []byte

//go:embed login.html
var loginPage []byte

//go:embed static
var staticFS embed.FS

func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
