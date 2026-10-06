package main

import (
	"embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Set is one TFT season shown as a card.
type Set struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Accent string `json:"accent"`
	Poster string `json:"poster"`
	Video  string `json:"video"`
	Info   string `json:"info"` // optional: path to a detail JSON (e.g. data/set18.json)
	Pbe    string `json:"pbe"`
	Live   string `json:"live"`
	Blurb  string `json:"blurb"`
	New    bool   `json:"new"`
}

type App struct{ fs embed.FS }

func NewApp(fs embed.FS) *App { return &App{fs: fs} }

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

// PlayGame starts the Riot Client straight into League of Legends (TFT is a mode inside it).
// Called from the UI: window.go.main.App.PlayGame()
func (a *App) PlayGame() error {
	path := riotClientPath()
	if path == "" {
		return errors.New("Riot Client not found. Is it installed?")
	}
	return exec.Command(path, "--launch-product=league_of_legends", "--launch-patchline=live").Start()
}

// riotClientPath asks Riot's own install registry first, then falls back to the default folder.
func riotClientPath() string {
	var candidates []string
	if pd := os.Getenv("ProgramData"); pd != "" {
		if b, err := os.ReadFile(filepath.Join(pd, "Riot Games", "RiotClientInstalls.json")); err == nil {
			var j struct {
				Default string `json:"rc_default"`
				Live    string `json:"rc_live"`
			}
			if json.Unmarshal(b, &j) == nil {
				candidates = append(candidates, j.Default, j.Live)
			}
		}
	}
	candidates = append(candidates, `C:\Riot Games\Riot Client\RiotClientServices.exe`)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
