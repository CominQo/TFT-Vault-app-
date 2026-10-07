# Privacy Policy - TFT Vault app

**Last updated: 2026-10-07**
**App: TFT Vault (CominQo) - https://github.com/CominQo/TFT-Vault-app**
**Riot API App ID: 888673**

## What data we collect
- **Riot Account:** We store only your Riot PUUID and GameName#TAG locally on your PC (%APPDATA%/tft-vault / localStorage). We never store your Riot password.
- **Match History:** Match IDs and basic stats fetched via Riot API (tft-match-v1) are cached locally for 5 minutes to reduce requests.
- **Friends:** Friend list (Name#TAG) is stored locally in your browser storage (tft.lang, tft.friends), never on our server.
- **No personal data selling:** We do not sell, trade, or share your data.

## Riot API usage
- Endpoints used: account-v1 (get PUUID), tft-match-v1 (match history), tft-league-v1 (rank), tft-status-v1
- Rate limiting: ~20 requests/min per user, cache 5 min, handles 429 with Retry-After header
- API key: Stored securely in server env (Go backend), never exposed in frontend or GitHub
- No cheating: App does not provide in-game overlay, automation, or unfair advantage. It is a collection tracker and team planner.

## Data retention
- Local only. You can delete %APPDATA%/tft-vault or clear browser storage to remove all data.
- We do not run a central database of user data (except optional standalone Go server for Friends & chat, which stores only friend requests, no Riot credentials).

## Contact
- GitHub Issues: https://github.com/CominQo/TFT-Vault-app/issues
- Donate: https://revolut.me/kubo_comor

## Third-party
- Riot Games is not responsible for this app. TFT and Riot Games are trademarks of Riot Games, Inc.
