package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Set is one TFT season shown as a card.
type Set struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Accent  string `json:"accent"`
	Poster  string `json:"poster"`
	Video   string `json:"video"`
	Trailer string `json:"trailer"` // optional: YouTube video id shown on the home screen
	Info    string `json:"info"`    // optional: path to a detail JSON (e.g. data/set18.json)
	Pbe     string `json:"pbe"`
	Live    string `json:"live"`
	Blurb   string `json:"blurb"`
	New     bool   `json:"new"`
}

type App struct {
	fs      embed.FS
	ctx     context.Context
	mu      sync.Mutex
	pending *ghRelease // the release found by the last CheckUpdate; InstallUpdate only ever installs this
	busy    bool
}

func NewApp(fs embed.FS) *App { return &App{fs: fs} }

// startup is called by Wails once the window exists.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cleanupOldBinary() // delete the leftover from the previous update
}

func (a *App) emit(name string, data ...interface{}) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, name, data...)
	}
}

// GetSets is called from the UI: window.go.main.App.GetSets()
// Add a new season by adding an entry to frontend/sets.json.
func (a *App) GetSets() ([]Set, error) {
	b, err := a.fs.ReadFile("frontend/sets.json")
	if err != nil {
		return nil, err
	}
	var sets []Set
	return sets, json.Unmarshal(b, &sets)
}

// ---------- auto-update (see updater.go) ----------

// GetVersion returns the version baked in at build time ("dev" for local builds).
func (a *App) GetVersion() string { return version }

// CheckUpdate looks for a newer release on GitHub. window.go.main.App.CheckUpdate()
func (a *App) CheckUpdate() (UpdateInfo, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	info, rel, err := checkUpdate(ctx, version)
	a.mu.Lock()
	a.pending = rel
	a.mu.Unlock()
	return info, err
}

// InstallUpdate downloads the release found by CheckUpdate, verifies it, swaps the exe and restarts.
// It takes no arguments on purpose: the UI cannot choose what gets downloaded.
// Progress is reported through the events "update:stage" (download|install|restart) and "update:progress" ({done,total}).
func (a *App) InstallUpdate() error {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return errors.New("an update is already running")
	}
	rel := a.pending
	a.busy = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.busy = false; a.mu.Unlock() }()

	if rel == nil || !installSupported {
		return errors.New("no installable update, check for updates first")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	newPath := exe + ".new"

	a.emit("update:stage", "download")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	var lastPct int64 = -1
	err = downloadVerified(ctx, rel, newPath, func(done, total int64) {
		if total > 0 {
			if pct := done * 100 / total; pct != lastPct {
				lastPct = pct
				a.emit("update:progress", map[string]int64{"done": done, "total": total})
			}
		}
	})
	if err != nil {
		os.Remove(newPath)
		return err
	}
	a.emit("update:stage", "install")
	if err := applyUpdate(exe, newPath); err != nil {
		os.Remove(newPath)
		return err
	}
	a.emit("update:stage", "restart")
	if err := restartApp(exe); err != nil {
		return errors.New("the update is installed, but the app could not restart itself. Please start it again")
	}
	go func() { time.Sleep(500 * time.Millisecond); wruntime.Quit(a.ctx) }()
	return nil
}
