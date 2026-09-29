package master

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamtew/ting/internal/config"
	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/store"
)

func TestAdoptSkipsExec(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(control.Status{Connected: true, Nick: "ting"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	db, err := store.Open(filepath.Join(t.TempDir(), "ting.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := db.Put(store.Bundle{Host: "irc.example.net", TLS: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutConnector(store.Connector{ServerID: b.ID, Listen: host, Token: "secret", PID: 99}); err != nil {
		t.Fatal(err)
	}

	m := New(config.Config{}, db, "", nil, log.New(io.Discard, "", 0))
	if err := m.StartServer(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.StopServer(b.ID) })
	if !m.running(b.ID) {
		t.Fatal("not running")
	}
	rt := m.withRuntime(b)
	if !rt.Running || rt.PID != 99 {
		t.Fatalf("%+v", rt)
	}
}
