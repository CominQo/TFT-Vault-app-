# TFT Vault server (friends + chat)

A tiny server with no dependencies (Go standard library only). It handles online accounts,
friend requests, chat messages, live updates and online status.

The app itself needs no server for everything else. Only the **Friends** page talks to this.

## Run it on your PC (testing)

```
cd server
go run .
```

It listens on `http://localhost:8080`. In the app open **Friends**, enter that address, then
**Create account**. (Both of you must reach the same server, so for real use deploy it, see below.)

## Deploy so friends can connect

Anywhere that runs a Docker container or a Go binary works: a small VPS, Fly.io, Render, Railway, ...

- **Persistent disk is required.** All accounts and messages live in one file (`DATA_FILE`,
  default `data.json`). The Dockerfile stores it in `/data`, so mount a volume there. Without
  one, everything is wiped on every redeploy.
- **Use HTTPS.** Passwords and messages travel to the server. Most hosts give you HTTPS for free.
  Plain `http://` is only OK for local testing.
- Environment variables: `PORT` (default `8080`), `DATA_FILE` (default `./data.json`).

```
docker build -t tftvault-server ./server
docker run -p 8080:8080 -v tftvault-data:/data tftvault-server
```

Once it's deployed, put its address in `frontend/index.html` so nobody has to type it:

```js
const DEFAULT_SERVER = 'https://your-server.example.com';
```

## Good to know

- Passwords are stored as salted PBKDF2 hashes. There is **no password reset** (no email).
- Messages are stored on the server **unencrypted**. Whoever runs the server can read them.
- Basic protection is built in: login/register attempts and message speed are rate limited.
- Each user keeps the last 500 messages per friend.
- This is an alpha-grade server. For lots of users you'd want a real database and backups.

## API (for reference)

All JSON. Authenticated calls send `Authorization: Bearer <token>`.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/register`, `/api/login` | `{username, password}` returns `{token, username}` |
| POST | `/api/logout` | invalidate the token |
| GET | `/api/state` | me, friends (online, unread), incoming and outgoing requests |
| POST | `/api/friends/add` `accept` `decline` `cancel` `remove` | `{username}` |
| GET | `/api/messages?with=name` | chat history |
| POST | `/api/messages` | `{to, text}` (max 500 chars) |
| POST | `/api/read` | `{with}` marks a chat as read |
| GET | `/api/stream` | live events (Server-Sent Events): `message`, `friend_request`, `friend_update`, `presence` |
