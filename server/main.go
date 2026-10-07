// TFT Vault friends & chat server.
// Standard library only. Accounts, friend requests, messages and live updates (Server-Sent Events).
//
//	PORT       listen port            (default 8080)
//	DATA_FILE  where data is stored   (default ./data.json) - put this on a persistent disk!
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// ---------- data ----------

type User struct {
	Name    string `json:"name"`
	Salt    string `json:"salt"`
	Hash    string `json:"hash"`
	Created int64  `json:"created"`
}
type Msg struct {
	ID   int64  `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
	TS   int64  `json:"ts"`
}
type Token struct {
	User string `json:"user"`
	Exp  int64  `json:"exp"`
}
type DB struct {
	Users    map[string]*User `json:"users"`
	Tokens   map[string]Token `json:"tokens"`
	Friends  map[string]bool  `json:"friends"`  // "a|b" (sorted, lowercase)
	Requests map[string]bool  `json:"requests"` // "from|to" pending
	Msgs     map[string][]Msg `json:"msgs"`     // per friend pair
	LastRead map[string]int64 `json:"lastRead"` // "user|with" -> ts
	NextID   int64            `json:"nextId"`
}

type sub struct{ ch chan []byte }

type Server struct {
	mu    sync.Mutex
	db    DB
	dirty bool
	path  string
	subs  map[string]map[*sub]struct{} // user key -> live connections
	rl    map[string][]time.Time       // rate limits
}

const (
	tokenTTL   = 90 * 24 * time.Hour
	pbkdfIters = 100_000
	maxMsgLen  = 500
	keepMsgs   = 500
)

var userRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,20}$`)

func newServer(path string) *Server {
	s := &Server{path: path, subs: map[string]map[*sub]struct{}{}, rl: map[string][]time.Time{}}
	s.db = DB{Users: map[string]*User{}, Tokens: map[string]Token{}, Friends: map[string]bool{}, Requests: map[string]bool{}, Msgs: map[string][]Msg{}, LastRead: map[string]int64{}, NextID: 1}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s.db); err != nil {
			log.Fatalf("cannot read %s: %v", path, err)
		}
	}
	if s.db.Users == nil {
		s.db.Users = map[string]*User{}
	}
	if s.db.Tokens == nil {
		s.db.Tokens = map[string]Token{}
	}
	if s.db.Friends == nil {
		s.db.Friends = map[string]bool{}
	}
	if s.db.Requests == nil {
		s.db.Requests = map[string]bool{}
	}
	if s.db.Msgs == nil {
		s.db.Msgs = map[string][]Msg{}
	}
	if s.db.LastRead == nil {
		s.db.LastRead = map[string]int64{}
	}
	if s.db.NextID == 0 {
		s.db.NextID = 1
	}
	return s
}

func (s *Server) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	b, err := json.Marshal(&s.db)
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		log.Println("save:", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Println("save:", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Println("save:", err)
	}
}
func (s *Server) saver(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flush()
		case <-ctx.Done():
			s.flush()
			return
		}
	}
}

// ---------- crypto ----------

func pbkdf2(pw, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, pw)
	hLen := prf.Size()
	n := (keyLen + hLen - 1) / hLen
	dk := make([]byte, 0, n*hLen)
	U := make([]byte, hLen)
	for block := 1; block <= n; block++ {
		prf.Reset()
		prf.Write(salt)
		prf.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		dk = prf.Sum(dk)
		T := dk[len(dk)-hLen:]
		copy(U, T)
		for i := 2; i <= iter; i++ {
			prf.Reset()
			prf.Write(U)
			U = prf.Sum(U[:0])
			for x := range U {
				T[x] ^= U[x]
			}
		}
	}
	return dk[:keyLen]
}
func randHex(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func hashPw(pw, salt string) string {
	return hex.EncodeToString(pbkdf2([]byte(pw), []byte(salt), pbkdfIters, 32))
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, e string) {
	writeJSON(w, code, map[string]string{"error": e})
}
func readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "bad_request")
		return false
	}
	return true
}
func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		return strings.TrimSpace(strings.Split(x, ",")[0])
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

// allow: at most n events per window for key
func (s *Server) allow(key string, n int, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	kept := s.rl[key][:0]
	for _, t := range s.rl[key] {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= n {
		s.rl[key] = kept
		return false
	}
	s.rl[key] = append(kept, now)
	return true
}
func pair(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func (s *Server) newToken(userKey string) string {
	t := randHex(32)
	s.mu.Lock()
	s.db.Tokens[t] = Token{User: userKey, Exp: time.Now().Add(tokenTTL).Unix()}
	s.dirty = true
	s.mu.Unlock()
	return t
}
func (s *Server) authKey(r *http.Request) (string, string) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.db.Tokens[tok]
	if !ok {
		return "", ""
	}
	if t.Exp < time.Now().Unix() {
		delete(s.db.Tokens, tok)
		s.dirty = true
		return "", ""
	}
	if _, ok := s.db.Users[t.User]; !ok {
		return "", ""
	}
	return t.User, tok
}

// publish sends an event to every live connection of a user. Caller must NOT hold s.mu.
func (s *Server) publish(userKey string, ev map[string]any) {
	b, _ := json.Marshal(ev)
	s.mu.Lock()
	subs := make([]*sub, 0, len(s.subs[userKey]))
	for c := range s.subs[userKey] {
		subs = append(subs, c)
	}
	s.mu.Unlock()
	for _, c := range subs {
		select {
		case c.ch <- b:
		default:
		} // slow client: drop; it refetches state on reconnect
	}
}
func (s *Server) name(key string) string {
	if u := s.db.Users[key]; u != nil {
		return u.Name
	}
	return key
}
func (s *Server) isOnline(key string) bool { return len(s.subs[key]) > 0 }

// friendsOf returns the lowercase keys of a user's accepted friends. Caller holds s.mu.
func (s *Server) friendsOf(me string) []string {
	var out []string
	for k := range s.db.Friends {
		p := strings.SplitN(k, "|", 2)
		if p[0] == me {
			out = append(out, p[1])
		} else if p[1] == me {
			out = append(out, p[0])
		}
	}
	sort.Strings(out)
	return out
}
func (s *Server) unread(me, with string) int {
	last := s.db.LastRead[me+"|"+with]
	n := 0
	for _, m := range s.db.Msgs[pair(me, with)] {
		if strings.ToLower(m.From) == with && m.TS > last {
			n++
		}
	}
	return n
}

// ---------- handlers ----------

type authFn func(w http.ResponseWriter, r *http.Request, me string)

func (s *Server) authed(fn authFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, _ := s.authKey(r)
		if me == "" {
			fail(w, 401, "unauthorized")
			return
		}
		fn(w, r, me)
	}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.allow("reg|"+clientIP(r), 10, time.Minute) {
		fail(w, 429, "rate")
		return
	}
	var in struct{ Username, Password string }
	if !readBody(w, r, &in) {
		return
	}
	if !userRe.MatchString(in.Username) {
		fail(w, 400, "bad_username")
		return
	}
	if len(in.Password) < 6 || len(in.Password) > 100 {
		fail(w, 400, "short_password")
		return
	}
	key := strings.ToLower(in.Username)
	salt := randHex(16)
	hash := hashPw(in.Password, salt)
	s.mu.Lock()
	if _, exists := s.db.Users[key]; exists {
		s.mu.Unlock()
		fail(w, 409, "taken")
		return
	}
	s.db.Users[key] = &User{Name: in.Username, Salt: salt, Hash: hash, Created: time.Now().Unix()}
	s.dirty = true
	s.mu.Unlock()
	writeJSON(w, 200, map[string]string{"token": s.newToken(key), "username": in.Username})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.allow("login|"+clientIP(r), 10, time.Minute) {
		fail(w, 429, "rate")
		return
	}
	var in struct{ Username, Password string }
	if !readBody(w, r, &in) {
		return
	}
	key := strings.ToLower(in.Username)
	s.mu.Lock()
	u := s.db.Users[key]
	s.mu.Unlock()
	salt, want := "0000000000000000", "x" // unknown user: still burn the same time
	if u != nil {
		salt, want = u.Salt, u.Hash
	}
	got := hashPw(in.Password, salt)
	if u == nil || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		fail(w, 401, "bad_creds")
		return
	}
	writeJSON(w, 200, map[string]string{"token": s.newToken(key), "username": u.Name})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, me string) {
	_, tok := s.authKey(r)
	s.mu.Lock()
	delete(s.db.Tokens, tok)
	s.dirty = true
	s.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type friendOut struct {
	Username string `json:"username"`
	Online   bool   `json:"online"`
	Unread   int    `json:"unread"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request, me string) {
	s.mu.Lock()
	out := map[string]any{"me": s.name(me)}
	friends := []friendOut{}
	for _, f := range s.friendsOf(me) {
		friends = append(friends, friendOut{s.name(f), s.isOnline(f), s.unread(me, f)})
	}
	in, outg := []string{}, []string{}
	for k := range s.db.Requests {
		p := strings.SplitN(k, "|", 2)
		if p[1] == me {
			in = append(in, s.name(p[0]))
		} else if p[0] == me {
			outg = append(outg, s.name(p[1]))
		}
	}
	sort.Strings(in)
	sort.Strings(outg)
	out["friends"], out["incoming"], out["outgoing"] = friends, in, outg
	s.mu.Unlock()
	writeJSON(w, 200, out)
}

// friendAction handles add / accept / decline / cancel / remove.
func (s *Server) friendAction(action string) authFn {
	return func(w http.ResponseWriter, r *http.Request, me string) {
		var in struct{ Username string }
		if !readBody(w, r, &in) {
			return
		}
		them := strings.ToLower(strings.TrimSpace(in.Username))
		status := "ok"
		s.mu.Lock()
		if _, ok := s.db.Users[them]; !ok {
			s.mu.Unlock()
			fail(w, 404, "no_user")
			return
		}
		if them == me {
			s.mu.Unlock()
			fail(w, 400, "self")
			return
		}
		pk := pair(me, them)
		var notify []map[string]any
		switch action {
		case "add":
			switch {
			case s.db.Friends[pk], s.db.Requests[me+"|"+them]:
				s.mu.Unlock()
				fail(w, 409, "already")
				return
			case s.db.Requests[them+"|"+me]: // they already asked us: becomes friends
				delete(s.db.Requests, them+"|"+me)
				s.db.Friends[pk] = true
				status = "accepted"
				notify = append(notify, map[string]any{"t": "friend_update"})
			default:
				s.db.Requests[me+"|"+them] = true
				status = "requested"
				notify = append(notify, map[string]any{"t": "friend_request", "from": s.name(me)})
			}
		case "accept":
			if !s.db.Requests[them+"|"+me] {
				s.mu.Unlock()
				fail(w, 404, "no_request")
				return
			}
			delete(s.db.Requests, them+"|"+me)
			s.db.Friends[pk] = true
			notify = append(notify, map[string]any{"t": "friend_update"})
		case "decline":
			delete(s.db.Requests, them+"|"+me)
		case "cancel":
			delete(s.db.Requests, me+"|"+them)
			notify = append(notify, map[string]any{"t": "friend_update"})
		case "remove":
			delete(s.db.Friends, pk)
			delete(s.db.Msgs, pk)
			notify = append(notify, map[string]any{"t": "friend_update"})
		}
		s.dirty = true
		s.mu.Unlock()
		for _, ev := range notify {
			s.publish(them, ev)
		}
		s.publish(me, map[string]any{"t": "friend_update"}) // keep my other windows in sync
		writeJSON(w, 200, map[string]string{"status": status})
	}
}

func (s *Server) getMessages(w http.ResponseWriter, r *http.Request, me string) {
	with := strings.ToLower(r.URL.Query().Get("with"))
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n < 300 {
		limit = n
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.db.Friends[pair(me, with)] {
		fail(w, 403, "not_friends")
		return
	}
	ms := s.db.Msgs[pair(me, with)]
	if ms == nil {
		ms = []Msg{} // JSON [] rather than null
	}
	if len(ms) > limit {
		ms = ms[len(ms)-limit:]
	}
	writeJSON(w, 200, ms)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request, me string) {
	var in struct{ To, Text string }
	if !readBody(w, r, &in) {
		return
	}
	to := strings.ToLower(in.To)
	text := strings.TrimSpace(in.Text)
	if text == "" || utf8.RuneCountInString(text) > maxMsgLen {
		fail(w, 400, "bad_text")
		return
	}
	if !s.allow("msg|"+me, 20, 10*time.Second) {
		fail(w, 429, "slow_down")
		return
	}
	s.mu.Lock()
	if !s.db.Friends[pair(me, to)] {
		s.mu.Unlock()
		fail(w, 403, "not_friends")
		return
	}
	m := Msg{ID: s.db.NextID, From: s.name(me), To: s.name(to), Text: text, TS: time.Now().UnixMilli()}
	s.db.NextID++
	pk := pair(me, to)
	ms := append(s.db.Msgs[pk], m)
	if len(ms) > keepMsgs {
		ms = ms[len(ms)-keepMsgs:]
	}
	s.db.Msgs[pk] = ms
	s.db.LastRead[me+"|"+to] = m.TS // my own message counts as read
	s.dirty = true
	s.mu.Unlock()
	ev := map[string]any{"t": "message", "msg": m}
	s.publish(to, ev)
	s.publish(me, ev)
	writeJSON(w, 200, m)
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request, me string) {
	var in struct{ With string }
	if !readBody(w, r, &in) {
		return
	}
	with := strings.ToLower(in.With)
	s.mu.Lock()
	s.db.LastRead[me+"|"+with] = time.Now().UnixMilli()
	s.dirty = true
	s.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// stream is a Server-Sent Events connection: messages, friend changes and presence.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, me string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "no_stream")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	c := &sub{ch: make(chan []byte, 64)}
	s.mu.Lock()
	first := len(s.subs[me]) == 0
	if s.subs[me] == nil {
		s.subs[me] = map[*sub]struct{}{}
	}
	s.subs[me][c] = struct{}{}
	friends := s.friendsOf(me)
	myName := s.name(me)
	s.mu.Unlock()
	if first {
		for _, f := range friends {
			s.publish(f, map[string]any{"t": "presence", "user": myName, "online": true})
		}
	}
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()
	defer func() {
		s.mu.Lock()
		delete(s.subs[me], c)
		last := len(s.subs[me]) == 0
		if last {
			delete(s.subs, me)
		}
		friends := s.friendsOf(me)
		s.mu.Unlock()
		if last {
			for _, f := range friends {
				s.publish(f, map[string]any{"t": "presence", "user": myName, "online": false})
			}
		}
	}()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case b := <-c.ch:
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		case <-ping.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*") // token travels in a header, no cookies
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	post := func(p string, fn http.HandlerFunc) { mux.HandleFunc("POST "+p, fn) } // other methods get 405
	get := func(p string, fn http.HandlerFunc) { mux.HandleFunc("GET "+p, fn) }
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("TFT Vault server is running.\n"))
	})
	post("/api/register", s.register)
	post("/api/login", s.login)
	post("/api/logout", s.authed(s.logout))
	get("/api/state", s.authed(s.state))
	for _, a := range []string{"add", "accept", "decline", "cancel", "remove"} {
		post("/api/friends/"+a, s.authed(s.friendAction(a)))
	}
	get("/api/messages", s.authed(s.getMessages))
	post("/api/messages", s.authed(s.postMessage))
	post("/api/read", s.authed(s.markRead))
	get("/api/stream", s.authed(s.stream))
	return cors(mux)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	path := os.Getenv("DATA_FILE")
	if path == "" {
		path = "data.json"
	}
	s := newServer(path)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.saver(ctx)
	srv := &http.Server{Addr: ":" + port, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second} // no WriteTimeout: SSE is long-lived
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("TFT Vault server listening on :%s (data: %s)", port, path)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	s.flush()
}
