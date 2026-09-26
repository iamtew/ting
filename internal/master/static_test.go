package master

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iamtew/tng/internal/config"
)

func TestStaticPublic(t *testing.T) {
	m := New(config.Config{}, nil, "", nil, log.New(io.Discard, "", 0))
	srv := httptest.NewServer(m.Handler())
	t.Cleanup(srv.Close)

	r, err := http.Get(srv.URL + "/static/css/admin.css")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("static: %d", r.StatusCode)
	}

	r2, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("index redirect follow: %d", r2.StatusCode)
	}
	if loc := r2.Request.URL.Path; loc != "/login" {
		t.Fatalf("want /login got %s", loc)
	}
}
