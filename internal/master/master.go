package master

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iamtew/ting/internal/acl"
	"github.com/iamtew/ting/internal/config"
	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/gateway"
	"github.com/iamtew/ting/internal/resolve"
	"github.com/iamtew/ting/internal/store"
)

const cookieName = "ting"
const logCap = 200

type logEvent struct {
	ServerID int64     `json:"server_id,omitempty"`
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	Line     string    `json:"line"`
}

type Master struct {
	cfg       config.Config
	db        *store.DB
	connector string
	stop      func()
	log       *log.Logger

	mu      sync.Mutex
	procs   map[int64]*proc
	engines map[int64]*resolve.Engine
	tail    []logEvent
	subs    map[chan logEvent]struct{}
}

func New(cfg config.Config, db *store.DB, connector string, stop func(), logger *log.Logger) *Master {
	if logger == nil {
		logger = log.Default()
	}
	return &Master{
		cfg:       cfg,
		db:        db,
		connector: connector,
		stop:      stop,
		log:       logger,
		procs:     make(map[int64]*proc),
		engines:   make(map[int64]*resolve.Engine),
		subs:      make(map[chan logEvent]struct{}),
	}
}

func (m *Master) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", staticHandler())
	mux.HandleFunc("GET /login", m.loginGET)
	mux.HandleFunc("POST /login", m.loginPOST)
	mux.HandleFunc("GET /", m.auth(m.index))
	mux.HandleFunc("GET /api/status", m.auth(m.apiStatus))
	mux.HandleFunc("GET /api/stream", m.auth(m.apiStream))
	mux.HandleFunc("GET /api/servers", m.auth(m.apiServersList))
	mux.HandleFunc("POST /api/servers", m.auth(m.apiServersCreate))
	mux.HandleFunc("GET /api/servers/{id}", m.auth(m.apiServersGet))
	mux.HandleFunc("PUT /api/servers/{id}", m.auth(m.apiServersPut))
	mux.HandleFunc("DELETE /api/servers/{id}", m.auth(m.apiServersDelete))
	mux.HandleFunc("POST /api/servers/{id}/start", m.auth(m.apiServersStart))
	mux.HandleFunc("POST /api/servers/{id}/stop", m.auth(m.apiServersStop))
	mux.HandleFunc("POST /api/servers/{id}/cycle", m.auth(m.apiServersCycle))
	mux.HandleFunc("POST /api/servers/{id}/import", m.auth(m.apiImportLinks))
	mux.HandleFunc("GET /api/links", m.auth(m.apiLinksList))
	mux.HandleFunc("POST /api/join", m.auth(m.apiJoin))
	mux.HandleFunc("POST /api/part", m.auth(m.apiPart))
	mux.HandleFunc("POST /api/privmsg", m.auth(m.apiPrivmsg))
	mux.HandleFunc("POST /api/nick", m.auth(m.apiNick))
	mux.HandleFunc("POST /api/raw", m.auth(m.apiRaw))
	mux.HandleFunc("POST /api/quit", m.auth(m.apiQuit))
	mux.HandleFunc("POST /api/shutdown", m.auth(m.apiShutdown))
	return mux
}

func Listen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func (m *Master) Serve(ln net.Listener) error {
	return http.Serve(ln, m.Handler())
}

func (m *Master) Boot(ctx context.Context) {
	if m.db == nil {
		return
	}
	list, err := m.db.List()
	if err != nil {
		m.log.Printf("list servers: %v", err)
		return
	}
	for _, b := range list {
		if b.Enabled {
			if err := m.StartServer(ctx, b.ID); err != nil {
				m.log.Printf("start %d: %v", b.ID, err)
			}
		}
	}
}

func (m *Master) onEvent(id int64, cl *control.Client, ev control.Event) {
	if ev.Kind != "line" {
		return
	}
	msg := gateway.Parse(ev.Line)
	if msg.Command != "PRIVMSG" {
		return
	}
	dest := ""
	if len(msg.Params) > 0 {
		dest = msg.Params[0]
	}
	text := msg.Last()
	if name, args, ok := ParseDot(text); ok {
		b, err := m.db.Get(id)
		if err != nil {
			return
		}
		role := acl.Role(b.Owners, b.Admins, msg.Nick, msg.User, msg.Host)
		if role == "" {
			return
		}
		st, _ := cl.Status()
		if msg.Nick != "" && msg.Nick == st.Nick {
			return
		}
		for _, a := range Dispatch(role, dest, msg.Nick, name, args, st) {
			if err := m.apply(cl, a); err != nil {
				m.log.Printf("%s: %v", a.Kind, err)
			}
		}
		return
	}
	if !isChan(dest) {
		return
	}
	st, _ := cl.Status()
	if msg.Nick != "" && msg.Nick == st.Nick {
		return
	}
	b, err := m.db.Get(id)
	if err != nil {
		return
	}
	reply := m.engineFor(id, b).HandleMessage(context.Background(), dest, text)
	if reply == "" {
		return
	}
	if err := cl.Privmsg(dest, reply); err != nil {
		m.log.Printf("resolve: %v", err)
	}
	raw := resolve.FirstURL(text)
	if err := m.db.InsertLink(id, dest, msg.Nick, raw, reply); err != nil {
		m.log.Printf("link: %v", err)
	}
}

func (m *Master) engineFor(id int64, b store.Bundle) *resolve.Engine {
	cfg := b.ResolveConfig()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.engines[id]; e != nil {
		e.UpdateConfig(cfg)
		return e
	}
	e := resolve.New(m.log, cfg)
	m.engines[id] = e
	return e
}

func (m *Master) syncEngine(id int64, b store.Bundle) {
	cfg := b.ResolveConfig()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.engines[id]; e != nil {
		e.UpdateConfig(cfg)
		return
	}
	m.engines[id] = resolve.New(m.log, cfg)
}

func (m *Master) dropEngine(id int64) {
	m.mu.Lock()
	delete(m.engines, id)
	m.mu.Unlock()
}

func (m *Master) apply(cl *control.Client, a Act) error {
	switch a.Kind {
	case "privmsg":
		return cl.Privmsg(a.Target, a.Text)
	case "join":
		return cl.Join(a.Target)
	case "part":
		return cl.Part(a.Target, "")
	case "mode":
		return cl.Raw("MODE " + a.Target + " " + a.Text)
	case "nick":
		return cl.Nick(a.Text)
	case "shutdown":
		return m.StopServer(m.idOf(cl))
	default:
		return nil
	}
}

func (m *Master) idOf(cl *control.Client) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.procs {
		if p.cl == cl {
			return id
		}
	}
	return 0
}

func (m *Master) push(ev logEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tail = append(m.tail, ev)
	if len(m.tail) > logCap {
		m.tail = m.tail[len(m.tail)-logCap:]
	}
	for ch := range m.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (m *Master) tokenOK(r *http.Request) bool {
	if c, err := r.Cookie(cookieName); err == nil && control.TokenOK(c.Value, m.cfg.Control.Token) {
		return true
	}
	got := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(got) > len(p) && control.TokenOK(got[len(p):], m.cfg.Control.Token) {
		return true
	}
	return false
}

func (m *Master) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.tokenOK(r) {
			if r.Header.Get("Accept") == "text/event-stream" || len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (m *Master) loginGET(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(loginPage)
}

func (m *Master) loginPOST(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tok := r.FormValue("token")
	if !control.TokenOK(tok, m.cfg.Control.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (m *Master) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexPage)
}

func (m *Master) apiStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, m.runtimeList())
}

func (m *Master) apiStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan logEvent, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	hist := append([]logEvent(nil), m.tail...)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}()
	for _, ev := range hist {
		if !writeSSE(w, fl, ev) {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if !writeSSE(w, fl, ev) {
				return
			}
		}
	}
}

type sidBody struct {
	ServerID int64  `json:"server_id"`
	Channel  string `json:"channel"`
	Reason   string `json:"reason"`
	Target   string `json:"target"`
	Text     string `json:"text"`
	Nick     string `json:"nick"`
	Line     string `json:"line"`
}

func (m *Master) clientFrom(w http.ResponseWriter, r *http.Request, body *sidBody) *control.Client {
	if !readJSON(w, r, body) {
		return nil
	}
	cl, err := m.client(body.ServerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil
	}
	return cl
}

func (m *Master) apiJoin(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	cl := m.clientFrom(w, r, &req)
	if cl == nil {
		return
	}
	if err := cl.Join(req.Channel); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiPart(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	cl := m.clientFrom(w, r, &req)
	if cl == nil {
		return
	}
	if err := cl.Part(req.Channel, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiPrivmsg(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	cl := m.clientFrom(w, r, &req)
	if cl == nil {
		return
	}
	if err := cl.Privmsg(req.Target, req.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiNick(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	cl := m.clientFrom(w, r, &req)
	if cl == nil {
		return
	}
	if err := cl.Nick(req.Nick); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiRaw(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	cl := m.clientFrom(w, r, &req)
	if cl == nil {
		return
	}
	if err := cl.Raw(req.Line); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiQuit(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	}
	cl, err := m.client(req.ServerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := cl.Quit(req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiShutdown(w http.ResponseWriter, r *http.Request) {
	var req sidBody
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	}
	if err := m.StopServer(req.ServerID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func (m *Master) apiServersList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, m.runtimeList())
}

func (m *Master) apiServersGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	b, err := m.db.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, m.withRuntime(b))
}

func (m *Master) apiServersCreate(w http.ResponseWriter, r *http.Request) {
	var b store.Bundle
	if !readJSON(w, r, &b) {
		return
	}
	b.ID = 0
	out, err := m.db.Put(b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if out.Enabled {
		if err := m.StartServer(r.Context(), out.ID); err != nil {
			m.log.Printf("start %d: %v", out.ID, err)
		}
	}
	m.syncEngine(out.ID, out)
	writeJSON(w, m.withRuntime(out))
}

func (m *Master) apiServersPut(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var b store.Bundle
	if !readJSON(w, r, &b) {
		return
	}
	b.ID = id
	old, err := m.db.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, err := m.db.Put(b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	running := m.running(id)
	switch {
	case !out.Enabled:
		if running {
			_ = m.StopServer(id)
		}
	case !running:
		if err := m.StartServer(r.Context(), id); err != nil {
			m.log.Printf("start %d: %v", id, err)
		}
	case store.SpecEqual(m.cfg.Identity, old, out):
	case store.DialEqual(m.cfg.Identity, old, out):
		cl, err := m.client(id)
		if err != nil {
			m.log.Printf("channels %d: %v", id, err)
			break
		}
		if err := cl.SyncChannels(out.Channels); err != nil {
			m.log.Printf("channels %d: %v", id, err)
		}
	default:
		_ = m.StopServer(id)
		if err := m.StartServer(r.Context(), id); err != nil {
			m.log.Printf("restart %d: %v", id, err)
		}
	}
	m.syncEngine(id, out)
	writeJSON(w, m.withRuntime(out))
}

func (m *Master) apiServersDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	_ = m.StopServer(id)
	m.dropEngine(id)
	if err := m.db.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiServersStart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := m.StartServer(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiServersStop(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := m.StopServer(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Master) apiServersCycle(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := m.CycleServer(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const importMax = 32 << 20

func (m *Master) apiImportLinks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := m.db.Get(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, importMax)
	var body io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(importMax); err != nil {
			http.Error(w, "file too large or bad multipart", http.StatusBadRequest)
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file required", http.StatusBadRequest)
			return
		}
		defer f.Close()
		body = f
	}
	links, skipped, err := store.ParseT3BLinks(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(links) == 0 && skipped == 0 {
		http.Error(w, "empty file", http.StatusBadRequest)
		return
	}
	st, err := m.db.ImportLinks(id, links)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st.Skipped = skipped
	writeJSON(w, st)
}

func (m *Master) apiLinksList(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("server_id"), 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "server_id required", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := r.URL.Query().Get("q")
	list, err := m.db.ListLinks(id, q, offset, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.Link{}
	}
	n, _ := m.db.CountLinks(id, q)
	writeJSON(w, map[string]any{"total": n, "offset": offset, "links": list})
}

func writeSSE(w http.ResponseWriter, fl http.Flusher, ev logEvent) bool {
	b, err := json.Marshal(ev)
	if err != nil {
		return false
	}
	if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
		return false
	}
	fl.Flush()
	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(dst); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
