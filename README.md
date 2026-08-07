# ⚔ Kingsreach

*March your king to the Gilded Throne.*

A **two-to-four player** medieval strategy game played on an embroidered honeycomb: race your
King (1) to the golden Throne in the centre, screened by your 2s and 3s. Take a rival's King, or
wall them in so no legal move remains, and they are out of the game — though their stones stay
on the board as obstacles. Last player standing, or first King to the Throne, wins.

Full rules: [docs/spelregels.md](docs/spelregels.md) (Dutch). Technical spec: [PROMPT.md](PROMPT.md).
The game is a homemade copy of **Kendo** (Ravensburger, 1989 — Keith Budden), whose published
rules settled the open questions in the Dutch text: eight pieces per player, no strikes in the
opening round, and knock-outs instead of instant defeat.

| Layer | Tech |
|---|---|
| Frontend | **Babylon.js + TypeScript** (`client/`) — 3D board for 2–4 players, pick up & drop stones with legal fields lit, orbit camera with a Focus button, dice roll for first player, captured stones beside the board, lobby browser with country flags, unlockable piece skins & environments |
| Backend | Go — authoritative rules engine, REST + polling, Steam sign-in, lobbies, computer players, profile progression, serves the frontend |
| Database | PostgreSQL (in-memory fallback when `DATABASE_URL` is empty) |
| Hosting | Docker (single container) — designed for **Easypanel** |

The bundle is ~1.6 MB (≈380 KB gzipped) and the Go server serves the pre-compressed file
automatically. A minimal 2D fallback client stays embedded in the binary at `/dev` — handy for
debugging the API without the 3D layer.

---

## Play

Sign in through **Steam** — your name and avatar come along — or just pick a name and play as a
guest. Then open the **board room**: every table waiting for players is listed with its host,
their flag, how many seats are filled and whether it needs a password. Take a seat, or open your
own table (2, 3 or 4 seats, name it, lock it if you like) and start when it fills — or hit
*Start now* and let the computer take the empty seats.

**Offline** is for a group round one screen: every seat is played from the same browser, taking
turns. It records nothing — no victories, no unlocks — which is the point of it.

Twenty victories earn a place in the **hall of champions**. Below that you do not appear on it at
all; the screen tells you how many wins you still need. Beating the computer counts.

Dice decide who opens and **you throw them yourself**: a die hovers over the board — click it
(or press *Throw the die*) and it tumbles. Every player throws one; highest opens, and a tie
sends just the tied players back to the dice. The value is rolled on the server the moment you
throw, so the animation is showing you a real result.

Drag one of your stones: it lifts off the cloth and every legal destination lights up; drop it on
one to move (or click the stone, then click a lit field). A stone you can **take** is circled in
red — click it and it is yours, no dragging needed. Drag anywhere else to orbit the board,
scroll to zoom, and press **Focus** to swing back to your own side of the table. Stones you lose
are laid out beside the board on your side, and the seat list on the left shows whose turn it is
and who has been knocked out.

Nobody may be struck during the opening round — everyone gets one safe move first.

**You have 150 seconds** to roll or move. The countdown sits in the top bar on your turn and turns
red near the end; run it out and you forfeit your seat, and the game goes to whoever is left — the
computer included. Offline games have no clock. Change it with `KINGSREACH_MOVE_SECONDS`.

The computer plays at **easy, medium or hard**, picked at random when it sits down, and pauses
before moving so it reads as an opponent rather than a script. Hard searches three plies with
alpha–beta and wins about ten games in ten against easy; easy takes the odd bad move and sometimes
misses a capture outright. Beating any of them counts towards the hall of champions.

When a game ends, **Rematch** sets the same table up again. Against the computer it starts at once;
against a person it waits until they press it too.

Online victories unlock piece skins (Royal Gold, Crystal Court, Runestones) and 3D environments
(Medieval Fair, Bombsite B, Boardgame Store, Streetside Café). Board finishes (Slate, Walnut,
Ivory, Forest, Ink) need nothing — they are a preference, not a prize. Open **Collection** in the
menu to equip any of it. Reach the top three of the hall of champions and your king wears a gold
crown at the table, biggest at number one. Progress is kept for players who signed in through Steam; guests play the same game
but keep nothing, which the menu says plainly rather than letting anyone find out the hard way.

Want everything available while working on the game? Start the server with
`KINGSREACH_UNLOCK_ALL=1` and every skin and environment is unlocked for every profile
(`dev.ps1` does this for you). The gate stays server-side, so switching the flag off restores
the real requirements.

---

## Run locally (Windows, no Docker needed)

```powershell
powershell -File dev.ps1
```

That starts the portable PostgreSQL in `.local/` (port 5433), builds the client bundle if it is
missing, and runs the server. Then open **http://localhost:8080**.

Working on the frontend? Run the bundler in watch mode next to it:

```bash
npm --prefix client run watch
```

Tests and checks:

```bash
go -C server test ./...
```

```bash
npm --prefix client run typecheck
```

Stop PostgreSQL with `.local\pgsql\bin\pg_ctl.exe -D .local\pgdata stop`.

## Run with Docker

```bash
docker compose up --build
```

The image builds the client and the server itself, so nothing generated needs to be committed.

---

## Deploy on Easypanel

1. Push this repo to Git (`web/` is build output and stays ignored).
2. Easypanel → **Create service → Postgres** (e.g. `kingsreach-db`, db/user `kingsreach`).
3. Easypanel → **Create service → App**, source = your repo, build type **Dockerfile**.
4. Environment:

   ```
   DATABASE_URL=postgres://kingsreach:<password>@kingsreach-db:5432/kingsreach?sslmode=disable
   PUBLIC_URL=https://your-domain.example
   TRUST_PROXY=1
   ```

   Optional, all with sensible defaults:

   | Variable | Default | What it does |
   |---|---|---|
   | `KINGSREACH_MOVE_SECONDS` | `150` | How long a player has to roll or move before forfeiting. `0` turns the clock off. |
   | `KINGSREACH_THINK_EASY` | `600-1400` | How long an easy computer player pauses before moving, in milliseconds, `min-max`. |
   | `KINGSREACH_THINK_MEDIUM` | `900-2200` | …a medium one. |
   | `KINGSREACH_THINK_HARD` | `1400-3500` | …a hard one. `0-0` makes bots answer instantly. |
   | `KINGSREACH_UNLOCK_ALL` | off | Every skin, board and place unlocked, for a preview deployment. |
   | `KINGSREACH_BOT_LOBBIES` | on | Set `0` to stop the server keeping its own tables open. |
   | `GEOIP_URL` | unset | Country lookup for IPv6 addresses; see below. |

   A malformed timing warns in the log and falls back to its default rather than
   refusing to boot. The values in use are printed at startup, so a typo shows
   up immediately instead of as odd behaviour hours later.

   (DB host = the name of the Postgres service on the internal network. `PUBLIC_URL` is the
   address players reach the game on; Steam sends them back to it after signing in, and without
   it Steam sign-in stays off and everyone plays as a guest. `TRUST_PROXY` makes the server
   believe `X-Forwarded-For` — correct behind Easypanel's router, and **wrong** anywhere the
   header is not set by something you control, since it is otherwise attacker input.)
5. Expose port **8080** and attach your domain. One container serves game + API.

## Signing in

Steam sign-in uses OpenID 2.0 — no API key, no Steam app registration, nothing to configure
beyond `PUBLIC_URL`. Players who would rather not sign in pick a name and a country and play as
a guest; their games work exactly the same, but nothing is saved, so unlockables stay locked.

## Country flags

The flag next to your name is a guess from your address. Click it on the menu to change it, or
click *set your flag* if there is none. It works out of the box and **no player address ever leaves the server**: a
2.1 MB IPv4→country table is compiled into the binary. Behind a CDN the `CF-IPCountry` header is
used instead, which is both cheaper and the only thing that covers IPv6.

Set `TRUST_PROXY=1` when something sits in front of the app, or every visitor looks like the
proxy. Optionally set `GEOIP_URL` to a service containing `{ip}` (e.g. `https://ipapi.co/{ip}/json/`)
to cover IPv6 addresses the built-in table cannot answer — that one *does* send the address to a
third party, which is why it is off by default.

To refresh the table: download `data/geoip2-ipv4.csv` from
[datasets/geoip2-ipv4](https://github.com/datasets/geoip2-ipv4) (it updates weekly), then

```bash
go run ./cmd/geoipgen -in geoip2-ipv4.csv -out internal/geoip/ipv4-country.bin
```

## Music

`client/src/music.ts` holds a `TRACKS` array — that array *is* the playlist. To add a song, put
the file in `client/static/audio/` and add a line with its title, artist and licence. The ♪ button
in the top-right corner mutes and unmutes, and the setting is remembered; muted players never
download the audio at all.

The bundled track is **"Magic Escape Room" by Kevin MacLeod** (incompetech.com), licensed
**CC BY 4.0** — that licence *requires* the credit, which is why the Collection screen renders it
from the track data. Keep the `licence` field accurate for anything you add. The file is 26 MB and
streams over range requests; if that matters for your bandwidth, re-encode it smaller, e.g.
`ffmpeg -i track.mp3 -c:a libopus -b:a 64k track.ogg`.

## Tables

There are no join codes. A player opens a named table — two to four seats, optionally behind a
password — and it appears in the **board room**: a scrollable list you can search by table, host
or country, filter to open or in-progress games, and sort by name, seats or age. To keep the room
from looking abandoned, the server keeps one to four tables of its own open, hosted by computer
players that take their turns once someone sits down, and pads the "in progress" list. See
PROMPT.md §3.5. Set `KINGSREACH_BOT_LOBBIES=0` to switch all of that off.

Those computer players get generated gamertags (`FrostRaven`, `marijke92`, `fr05tr4v3n`) and name
their tables **in their own language** — "iemand zin in een potje?", "wer traut sich",
"誰か一緒にどうですか" — in the register people actually type, lowercase and all. Add a language
by putting its code in `hostOrigins` and its phrases in `tableNamesByLang`, both in
`server/internal/httpapi/names.go`.

## API in one breath

`POST /api/games` `{mode, players, profile, name, password}` → state+token · `POST /api/games/join`
`{gameId, password, profile}` → state+token · `POST /api/games/{id}/start` · `GET /api/lobbies?token`
→ `{lobbies, running}` · `GET /api/board` → graph · `GET /api/config` → `{steam, unlockAll}` ·
`GET /api/games/{id}?token&v` → state or 204 · `GET /api/games/{id}/moves?token&from` → legal moves ·
`POST /api/games/{id}/move` `{token,from,to}` → state · `POST /api/games/{id}/roll` ·
`POST /api/games/{id}/resign` · `GET /api/leaderboard?token` · `GET /api/catalog` ·
`POST /api/profile` `{name, country}` ·
`GET /api/profile?token` · `POST /api/profile/update` · `POST /api/profile/equip` ·
`GET /api/auth/steam/login` · `GET /api/auth/steam/return` · `GET /healthz`.

## Repository layout

```
client/    Babylon.js frontend (TypeScript, esbuild → web/)
  static/models/   imported .glb scenery, loaded on demand
server/    Go module: rules engine, REST API, static hosting, embedded /dev client
web/       build output (generated — not committed)
docs/      original Dutch rules
```

### Third-party assets

Everything borrowed here is **Creative Commons Attribution**, which means the credit is a
condition of use, not a nicety — so it is shown in the game's Collection screen, rendered from
`client/src/credits.ts` and `client/src/music.ts` rather than hard-coded. Add an asset, add a row.

| Asset | Work | By | Licence |
|---|---|---|---|
| `client/static/models/de-dust2.glb` | [de_dust2 - CS map](https://sketchfab.com/3d-models/de-dust2-cs-map-056008d59eb849a29c0ab6884c0c3d87) | pancakesbassoondonut (Sketchfab) | CC BY 4.0 |
| `client/static/models/dice.glb` | [Dice](https://sketchfab.com/3d-models/dice-3b955af797e140eca0947ede57f412ba) | tnRaro (Sketchfab) | CC BY 4.0 |
| `client/static/audio/magic-escape-room.mp3` | Magic Escape Room | Kevin MacLeod (incompetech.com) | CC BY 4.0 |
| `server/internal/geoip/ipv4-country.bin` | GeoLite2 Country data, via [datasets/geoip2-ipv4](https://github.com/datasets/geoip2-ipv4) | MaxMind | GeoLite2 EULA — credit required |

One caveat on the map: the CC-BY licence is the uploader's, and covers the conversion work they
did. The underlying *de_dust2* level and its textures are Valve Corporation's, and a third party
cannot place those under CC-BY. If this is ever deployed publicly rather than kept private,
that is worth resolving — replacing the model is the easy path, since `buildDust2` in
`client/src/environments.ts` is the only thing that loads it.

`client/static/images/steam-signin.png` is Valve's own sign-in button from
[steamcommunity.com/dev](https://steamcommunity.com/dev), served from our origin rather than
hotlinked so no visitor's address reaches Valve before they choose to sign in. It is shown at its
native 180×35 and never recoloured — the "not associated with Valve Corp." line is part of the
image and has to stay legible.

`client/static/audio/magic-escape-room.mp3` is **"Magic Escape Room" by Kevin MacLeod**
(incompetech.com), **CC BY 4.0** — attribution is a licence condition and is shown in the
Collection screen. The 1.4 MB of embedded cover art was stripped (the browser must read the whole
ID3 tag before reaching the first audio frame); the audio itself is untouched.

Rule discrepancy note: the rules text says 10 pieces per player, but the setup diagram and the
photo of the physical set both show 8 (1 King, 3×2, 4×3) — the implementation follows the
diagram/photo. Change `westSetup` in `server/internal/game/state.go` to adjust.
