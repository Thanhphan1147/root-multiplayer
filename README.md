# Root Machine Notation — Multiplayer

Correspondence multiplayer for [Root Machine Notation](https://github.com/Thanhphan1147/root-mn)
(RMN), the machine-first notation + rules engine for the board game ROOT.

Create a room, share one link per player, and play a full game of ROOT across
devices on your own schedule. No accounts, no sign-ups — each seat gets its own
secret token.

- **Serverless-ish:** one small Go server; game state is the authoritative engine
  JSON plus an appended `.rmn` log per room.
- **Seat tokens:** the creator picks the player count (2–4) and gets one signed
  token per seat. Tokens are JWTs embedding the room and seat, and are also the
  authentication.
- **Faction picks:** each player picks an available faction on first join; the
  pick is recorded in the room's `.rmn` header (`%Faction C marquise seat=1`).
- **Turn-enforced:** the server rejects any write that does not come from the
  current player's token.
- **Hidden information:** each player receives a redacted view — other hands and
  supporters are masked, the deck is removed, and hidden outcomes (draws/deals)
  are scrubbed until they become public.
- **Correspondence-friendly:** auto-fetch (configurable interval) plus `ETag` /
  `304 Not Modified`, so nothing is transferred when the state hasn't changed.
- **Opt-in live mode:** a "live" toggle opens a WebSocket that only *notifies*
  when the room changes; the client still fetches over HTTP, so redaction and
  caching stay in one place. If the socket drops it silently falls back to
  polling.

## Run it

### Docker (recommended)

```sh
RMN_SECRET="$(openssl rand -hex 32)" docker compose up -d --build
# open http://localhost:8080
```

Room data lives in the `rmn-data` volume.

### From source

```sh
go run ./cmd/server --addr :8080 --data ./data --web ./web
```

Set `RMN_SECRET` to a long random string in production (it signs every token).

## How a game flows

1. Someone opens the client and clicks **Create room**, choosing 2–4 players.
2. The client shows one link (or token) per seat. Send each player their own.
3. Each player opens their link, pastes their token if needed, and picks a
   faction. When every seat has a faction, the game starts.
4. On your turn the client shows your legal actions; submit one and the server
   applies it, saves the room, and returns your redacted view. Everyone else
   sees the change on their next fetch.
5. **Leave** clears your local token; keep it to rejoin later. **Export** (via
   `GET /api/export`) returns the room's `.rmn` log.

## API

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/api/rooms` | — | `{players, names?}` → `{room, tokens[]}` |
| `GET` | `/api/state` | token | Redacted view + `ETag` (304 on `If-None-Match`) |
| `POST` | `/api/faction` | token | `{faction}` → pick a faction (lobby) |
| `POST` | `/api/action` | token | `{id}` → apply a legal action (turn-checked) |
| `GET` | `/api/export` | token | The room's `.rmn` log |
| `GET` | `/api/ws` | token* | WebSocket change notifications (opt-in live mode) |
| `GET` | `/api/health` | — | Liveness |

Auth is `Authorization: Bearer <token>` (or `?token=`). The WebSocket accepts
the token in a first frame (`{"token":"..."}`) or as `?token=`, and only pushes
`{"type":"changed","seq":N}` nudges.

## Design notes

- **State authority:** the engine state JSON is authoritative; the `.rmn` log is
  regenerated from the header + the engine's event log on every save, so the two
  never drift.
- **Hidden information vs deterministic logs:** RMN records explicit outcomes
  (exactly which cards were drawn), which would leak hidden info. The server
  keeps the full log and redacts it per viewer.
- **Scope:** base game, four factions. Expansion factions and a WebSocket push
  channel are natural next steps.

## Development

```sh
go build ./...
go test ./...
gofmt -l .
go vet ./...
```

## License

[MIT](LICENSE) © 2026 Thanh Phan.

ROOT is designed by Cole Wehrle and published by Leder Games. This is an
unofficial fan project, not affiliated with or endorsed by Leder Games.
