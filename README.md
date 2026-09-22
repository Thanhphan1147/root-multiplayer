# Root Machine Notation — Multiplayer

Correspondence multiplayer for [Root Machine Notation](https://github.com/Thanhphan1147/root-mn)
(RMN), the machine-first notation + rules engine for the board game ROOT.

The host sets up tables (rooms) from a small CLI; players take a seat at a
table from the browser and play a full game of ROOT across devices on their own
schedule. No accounts, no sign-ups — each seat gets its own secret token.

- **Serverless-ish:** one small Go server; game state is the authoritative engine
  JSON plus an appended `.rmn` log per room.
- **Host-arranged rooms:** rooms are created only from the administrator CLI, so
  the host controls the tables. Every table has four seats and a game can start
  with 2–4 of them taken.
- **Seat tokens:** taking a seat issues a signed JWT for that seat, stored by the
  browser. Leaving frees the seat and rotates its token, so the next player gets
  a fresh one.
- **Faction picks:** each player picks an available faction after taking a seat;
  the pick is recorded in the room's `.rmn` header (`%Faction C marquise seat=1`).
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

# Set up a table for players (host only):
docker exec root-multiplayer rmn-mp create-room --data /app/data --name "Friday game"

# open http://localhost:8080
```

Room data lives in the `rmn-data` volume. Players open the site and click a seat.

### HTTPS with Tailscale Funnel

Serving over HTTPS is recommended: it makes the browser clipboard work on
mobile (copy buttons) and is the normal way to expose a self-hosted server
without opening ports.

If the host running the server is already joined to a Tailscale network
(tailnet), you can get a free, stable HTTPS URL without owning a domain.
Tailscale Funnel exposes `127.0.0.1:8080` to the public internet at
`https://<host>.<tailnet>.ts.net` and provisions the TLS certificate for you.

Requirements: MagicDNS and HTTPS certificates enabled for the tailnet, plus the
`funnel` node attribute in the tailnet policy file. The first `tailscale funnel`
run walks you through enabling both.

```sh
# The default compose file publishes 8080 on the host, which Funnel proxies.
tailscale funnel --bg 8080
# Available on the internet:
# https://<host>.<tailnet>.ts.net

tailscale funnel status          # show the current Funnel
tailscale funnel --bg 8080 off   # stop sharing
```

Funnel is free on all plans but is in beta, is bandwidth-limited (fine for a
turn-based game), and only listens on ports `443`, `8443`, and `10000`. Because
the URL is stable, share the link once and reuse it across games. `--bg` keeps
it running across reboots; WebSockets are supported, and the server's 25s pings
keep the live socket alive.

Test the URL from a device outside the tailnet (for example, a phone on
cellular). On the server host itself, MagicDNS resolves the Funnel name to the
host's own Tailscale IP, so a local `curl` may hit another service that is bound
to `*:443` (such as HAProxy) instead of the Funnel.

Security notes: `RMN_SECRET` stays on the server and tokens travel over HTTPS.

### Administrator CLI

Rooms are created and managed with the same binary, run against the server's data
directory. In Docker:

```sh
docker exec root-multiplayer rmn-mp create-room --data /app/data --name "Friday game"
docker exec root-multiplayer rmn-mp rooms --data /app/data
docker exec root-multiplayer rmn-mp kick --data /app/data --room <room-id> --seat 2
```

`create-room` makes a four-seat table. `rooms` lists every table and which seats
are free, taken, or faction-picked. `kick` empties a seat and, once the game has
started, drops that faction's pieces from the board, for when a player has gone
quiet; the table keeps its four seats. Room ids and seat numbers are printed by
`create-room` and `rooms`.

To give a lone player an opponent, fill a seat with the server-side bot:

```sh
docker exec root-multiplayer rmn-mp add-bot --data /app/data --room <id> --seat 2 --faction ED --bot greedy:full
```

`--faction` is optional (defaults to the first free faction). `--bot` accepts
`random`, `passive`, `greedy:<profile>`, or `mcts:<profile>`. The bot picks a
faction, plays its own turns on the server, and is marked in gold in the lobby.
Bot moves are computed by [root-bot](https://github.com/Thanhphan1147/root-bot).

### From source

```sh
go run ./cmd/server serve --addr :8080 --data ./data --web ./web
```

Set `RMN_SECRET` to a long random string in production (it signs every token).

## How a game flows

1. The host creates a table from the CLI (`create-room`). Every table has four
   seats.
2. Players open the site and see each table as a top-down board with four chairs.
   Clicking a free chair takes that seat and stores its token; 2–4 players can sit
   down.
3. Each seated player picks an available faction. Once every seated player has a
   faction, the game starts with however many took a seat; the unused chairs close.
4. On your turn the client shows your legal actions; submit one and the server
   applies it, saves the room, and returns your redacted view. Everyone else
   sees the change on their next fetch.
5. **Leave** frees your seat and rotates its token; anyone can take it again from
   the lobby. **Export** (via `GET /api/export`) returns the room's `.rmn` log.

## API

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/api/rooms` | — | Every table with its seats → `{rooms[]}` |
| `POST` | `/api/take` | — | `{room, seat}` → take a free seat → `{room, seat, token}` |
| `POST` | `/api/leave` | token | Free your seat and revoke its token |
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
- **Seat tokens & revocation:** a token embeds the seat's stable id and a version.
  Leaving or ejecting bumps the version, so an old token stops working while the
  other seats keep theirs.
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
