# TFT Collection (Go + Wails, native Windows) - TFT Vault



https://github.com/user-attachments/assets/97550b77-cc72-4ed5-9f1e-fa062f8ea92d





[TFT Vault](https://img.shields.io/badge/TFT-Set%2018%20Enchanted%20Wilds-3ab66f) [Go](https://img.shields.io/badge/Go-1.22%2B-blue) [Wails](https://img.shields.io/badge/Wails-v2-red)

Requires: Go 1.22+, Wails v2 (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`), WebView2 (ships with Windows 10/11).

```bash
go mod tidy
wails dev        # live window
wails build      # -> build/bin/tft-collection.exe
```

## What is TFT Vault?
- **TFT Collection tracker** - your collection, sets overview
- **Team Planner** - Set 18 Enchanted Wilds ready (65 champions: 14x 1-cost, 13x 2-cost, 14x 3-cost, 14x 4-cost, 10x 5-cost), traits, augments, Wisps mechanic
- **Match History** - recent games (via Riot API)
- **Comp Builder** - plan team comps (coming soon)
- **Battle Pass tracker** - custom Vault-style dock (bottom-left, above logo/EN/Donate)
- **Friends & chat** - standalone Go server (accounts, friend requests)

## Set 18 - Enchanted Wilds (latest)
- Release: Live 26.08.2026, PBE 12.08.2026
- Engine: First set on Unreal Engine 5
- Mechanic: **Wisps** - every 2 shop rerolls, one-time effects (gold, XP, combat buffs, high-risk high-reward). Blossom trait empowers Wisps
- Traits: Riftbeast (Elder Dragon counts as 2, 2 slots), Sprykin (giant furry friend ride), Old Growth (infinite HP scaling), Emerald Aspect, Inferno, Blossom, Fae, Elderwood, Juggernaut, Vanguard, Brawler, Executioner, Spellweaver, Hunter, etc.
- 5-costs: Maokai, Taric, Elder Dragon, Gnar, Kennen, Alune, Ashe, Ivern, Lux (Avatar - 9 variations), Draven (Bounty Seeker quests)
- Full data: `frontend/data/set18.json` and `frontend/data/tft_set18_team_planner.json` (champions + traits + wisps + augments)

## Adding a season
- Add new entry in `frontend/sets.json`. Add `"info": "data/setNN.json"` to give it an Overview / Champions / Traits page (see `frontend/data/set18.json` for format).
- Put the Pengu clip at `frontend/video/pengu.mp4`.
- Card art: set `"poster"` to an image path inside `frontend/`.

## Riot API - Production Key Application (App 888673)
**For Riot reviewer:**

- **Product:** TFT Vault - Collection tracker & Team Planner (native Windows)
- **Description:** Native Wails app for viewing TFT collection, planning comps for Set 18 (65 champs), viewing match history. Does NOT provide in-game overlay or unfair advantage.
- **Endpoints used:**
  - `account-v1` - get PUUID by Riot ID
  - `tft-match-v1` - match history
  - `tft-league-v1` - rank
  - `tft-status-v1` - server status
- **Rate limit handling:**
  - ~20 requests/min per user max
  - Local cache 5 minutes for match data
  - Handles 429 with Retry-After header sleep + exponential backoff
  - API key stored in server env var `RIOT_API_KEY`, never in frontend or repo
  - User-Agent: `TFT-Vault-app/1.0 (CominQo)`
- **Privacy Policy:** [PRIVACY.md](PRIVACY.md)
- **Terms:** [TERMS.md](TERMS.md)
- **Contact:** GitHub Issues
- **Demo:** Build exe or `wails dev`

## Development
- Frontend: `frontend/index.html` (vanilla JS, no framework)
- Backend: `app.go`, `main.go` (Wails)
- Data: `frontend/data/` - set JSONs
- Pass tracker: `pass-track-vault.html` merged into index (dock bottom-left, hex outline orange)

## Donate
If you like the app: https://revolut.me/kubo_comor

## Legal
Riot Games, TFT, Teamfight Tactics are trademarks of Riot Games, Inc. This project is not endorsed by Riot Games.

## Releasing an update (auto-update)

The app checks GitHub Releases on launch and offers new versions in a popup (and via the refresh button
next to Donate). To publish one:

```
git tag v0.1.3
git push origin v0.1.3
```

The workflow builds `TFT-Vault.exe`, bakes in the version, and publishes a Release with the exe and its
`.sha256` checksum. The app only installs a release that has both, and only if the checksum matches.
Release notes (auto-generated from commits) are shown in the update popup. Local/untagged builds are
`dev` and never offer updates. Auto-install works on Windows; elsewhere the popup links to the release page.
