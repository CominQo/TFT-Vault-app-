# TFT Collection (Go + Wails, native Windows)

Requires: Go 1.22+, Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`), WebView2 (ships with Windows 10/11).

    go mod tidy
    wails dev        # live window
    wails build      # -> build/bin/tft-collection.exe

- Add a season: new entry in `frontend/sets.json`. Add `"info": "data/setNN.json"` to give it an Overview / Champions / Traits page (see `frontend/data/set18.json` for the format).
- Put the Pengu clip at `frontend/video/pengu.mp4`.
- Card art: set `"poster"` to an image path inside `frontend/`.
