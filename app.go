package main

import (
	"embed"
	"encoding/json"
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
