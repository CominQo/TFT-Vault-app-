package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Set at build time (see build.yml):
//
//	-ldflags "-X main.modelsRemote=https://raw.githubusercontent.com/OWNER/REPO/BRANCH/frontend/models/"
//
// Empty = disk only.
var modelsRemote string

type modelsHandler struct {
	dirs   []string
	cache  string
	client *http.Client
	locks  sync.Map // rel path -> *sync.Mutex
}

func newModelsHandler() http.Handler {
	h := &modelsHandler{client: &http.Client{Timeout: 10 * time.Minute}}
	if e := os.Getenv("TFT_MODELS_DIR"); e != "" {
		h.dirs = append(h.dirs, e)
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		b := filepath.Dir(exe)
		h.dirs = append(h.dirs, filepath.Join(b, "models"), filepath.Join(b, "frontend", "models"))
	}
	if wd, err := os.Getwd(); err == nil { // wails dev
		h.dirs = append(h.dirs, filepath.Join(wd, "frontend", "models"))
	}
	c, err := os.UserCacheDir()
	if err != nil {
		c = os.TempDir()
	}
	h.cache = filepath.Join(c, "TFT Vault", "models")
	return h
}

// validModelPath accepts only "<folder>/<file>.glb" with no traversal.
func validModelPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || !strings.HasSuffix(strings.ToLower(parts[1]), ".glb") {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `\:*?"<>|`) {
			return false
		}
	}
	return true
}

func (h *modelsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, "/models/")
	if rel == r.URL.Path || !validModelPath(rel) {
		http.NotFound(w, r)
		return
	}
	local := filepath.FromSlash(rel)
	for _, d := range append(append([]string{}, h.dirs...), h.cache) {
		p := filepath.Join(d, local)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			h.serve(w, r, p)
			return
		}
	}
	if modelsRemote == "" {
		http.NotFound(w, r)
		return
	}
	dst := filepath.Join(h.cache, local)
	if err := h.fetch(rel, dst); err != nil {
		println("model download failed:", rel, err.Error())
		http.NotFound(w, r)
		return
	}
	h.serve(w, r, dst)
}

func (h *modelsHandler) serve(w http.ResponseWriter, r *http.Request, p string) {
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, p)
}

// fetch downloads one model from the repo into the user cache (atomic, de-duplicated).
func (h *modelsHandler) fetch(rel, dst string) error {
	l, _ := h.locks.LoadOrStore(rel, &sync.Mutex{})
	mu := l.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	segs := strings.Split(rel, "/")
	for i := range segs {
		segs[i] = url.PathEscape(segs[i])
	}
	resp, err := h.client.Get(strings.TrimRight(modelsRemote, "/") + "/" + strings.Join(segs, "/"))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err == nil && resp.ContentLength > 0 && n != resp.ContentLength {
		err = fmt.Errorf("short download")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
