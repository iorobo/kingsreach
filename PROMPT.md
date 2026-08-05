# KINGSREACH — Development Prompt

> **Game name:** **Kingsreach** — *"March your king to the Gilded Throne."*
> A medieval strategy board game for two players, played on an embroidered honeycomb.
> (Alternate names considered: *Hollowcrown*, *Gildpath*, *Thronemoot*.)

This document is the complete, self-contained prompt/spec for building the game. Hand it to a
developer (or an AI agent) and they can build the whole product without further context.

---

## 1. Product summary

Build a 2-player online board game called **Kingsreach**:

- **Frontend:** Babylon.js + TypeScript (`client/`), bundled with esbuild into `web/`.
  Procedurally generated 3D board, pieces and environments — no art assets. HTML/CSS overlay for
  all text UI. Talks to the backend over plain HTTP (polling, turn-based game).
  *(A Unity WebGL client was built first and still lives in `unity/` for reference; the shipped
  frontend is the Babylon one — a ~380 KB gzipped bundle instead of ~15 MB, and no Editor in the
  build chain.)*
- **Backend:** Go (single binary). Server-authoritative rules engine, REST API, serves the Unity
  WebGL build as static files. Also embeds a lightweight HTML/JS **dev client** at `/dev` for
  testing the full game loop without a Unity build.
- **Database:** PostgreSQL (games + move history). In-memory fallback when `DATABASE_URL` is empty.
- **Deployment:** One `Dockerfile` (multi-stage) + `docker-compose.yml`. Designed for
  **Easypanel**: app service from the Dockerfile + managed Postgres service.

Two game modes:

| Mode | Description |
|---|---|
| `online` | The creator opens a named table; others find it in the lobby browser and take a free seat. Optionally password-protected. The only mode that records anything. |
| `offline` | One browser runs every seat, so a group can share a screen. **Records nothing** — no played games, no victories, no unlocks. It was called `practice`; the old name is still accepted on input and normalised. |

---

## 2. Game rules (canonical, English)

Sources: `docs/spelregels.md` (the Dutch original), the photo of the physical board, and the
game it is a homemade copy of — **Kendo**, Ravensburger 1989, designed by Keith Budden: a
2–4 player race on 19 hexagons where each player has one prince, three samurai and four
fighters, and wins by walking their prince into the palace at the centre. That published game
settled two questions the Dutch text left open (see §2.5).

### 2.1 The board — IMPORTANT interpretation

The physical board is a hexagon-shaped cloth embroidered with a honeycomb of **19 hexagons**
(a hexagonal patch: side 3, axial radius 2). **Pieces do not stand inside the hexagons — they
stand on the line intersections (the vertices of the honeycomb).** This is unambiguous from the
photo: pieces sit on the stitched intersections, and the golden star in the middle connects the
**center point** of the central hexagon to its **six corners**.

Therefore the playing graph is:

- **54 regular fields** = the vertices of the 19-hex honeycomb.
- **1 goal field ("the Throne")** = the center point of the middle hexagon.
- **72 regular connections** = the embroidered hexagon edges (teal).
- **6 golden connections ("Gilded Paths")** = Throne ↔ each corner of the central hexagon.
- Total: **55 fields, 78 connections.** Regular fields have degree 2–3; the Throne has degree 6.

Integer coordinate system (used by both server and clients):

- Hex with axial coords `(q, r)`, `max(|q|,|r|,|q+r|) ≤ 2` → 19 hexes.
- Hex center key: `(3q, q + 2r)`.
- Corner keys = center key + one of `(±2,0), (±1,±1)` (E, NE, NW, W, SW, SE).
- Node id = `"kx,ky"` string. Render position: `x = kx/2`, `y = ky·√3/2` (edge length 1, y up).
- The Throne is `(0,0)` (not a honeycomb vertex — no collision).

The two players start on the **west** and **east** sides of the board (vertical zigzag edges).

### 2.2 Pieces

Each player has **8 pieces**: 1 King (value 1), 3 pieces of value 2, 4 pieces of value 3.

> **Note / discrepancy:** the Dutch rules text mentions "10 pieces (1×1, 3×2, 6×3)", but both the
> setup diagram in the same document and the photo of the physical game show **8** pieces
> (1×1, 3×2, 4×3) per player. The diagram and photo agree exactly, so 8 is implemented.
> The setup is a single data table in `server/internal/game/state.go` — trivially adjustable.

Starting setup (west player; east is mirrored, `kx → -kx`). Matches the rules diagram
(bottom row = the player's back edge) and the photo:

| Piece | Node key |
|---|---|
| King (1) | `-8,0` (the westernmost field) |
| 3 | `-8,-2` · `-7,-1` · `-7,1` · `-8,2` (flanking the king along the edge) |
| 2 | `-5,-1` · `-4,0` · `-5,1` (the inner row) |

### 2.3 Movement

- A move = pick one of your pieces, move it **exactly** its value in steps (1, 2 or 3) along
  connections. Turning during the move is allowed (any shape, not just straight lines).
- **No revisiting**: within one move, a piece may never enter a field it already visited during
  that same move (including its starting field).
- **No jumping**: every intermediate field must be **empty** (friend and foe both block).
- **Capture**: ending exactly on a rival-occupied field removes that piece and takes its place.
  Capturing is never mandatory. You may never end on your own piece.
- **Opening protection**: no piece may be struck until every player has had one turn (Kendo:
  "from the second round, pieces may be captured"). The server refuses such a move with a
  distinct error so the client can explain why, and never offers it as a legal destination.
- **King restrictions**: only the King may traverse the 6 golden connections and only the King
  may enter the Throne. All other pieces treat the Throne + golden paths as nonexistent.

### 2.4 Knock-outs and game end

A player is **knocked out** — not removed — when their King is captured, when they have no
legal move on their turn, or when they resign. Their pieces **stay on the board as obstacles**
and can still be struck, but never move again. This is Kendo's rule and it is what makes the
multiplayer game work.

The game ends when:

1. **Win** — a King reaches the Throne (`throne`). Immediate, whatever else is happening.
2. **Win** — only one player is still in (`last-standing`). In a two-player game that is the
   old "king captured / opponent blocked" ending.
3. *(server safeguard, house rule)* after 800 plies it is a draw (`move-limit`).

**Who begins** is decided by dice, and **the players throw them**. A game opens in its `roll`
phase: every seat owes one throw and no piece may move (`ErrStillRolling`). Clicking the die —
or the *Throw the die* button — calls `POST /api/games/{id}/roll`, and the value is generated
**at that moment**, server-side. The highest single throw opens; if the lead is shared, only the
tied seats throw again. The animation lands on the value the server returned, so the tumble
shows a real result rather than replaying one decided earlier.

### 2.5 Seats and player counts

The board has six-fold rotational symmetry: rotating a key `(kx,ky)` by 60°
(`(kx,ky) → ((kx−3ky)/2, (kx+ky)/2)`) maps it onto itself. Every seat therefore reuses the west
formation, rotated — there is only one setup table in the code.

| Players | Seats (clockwise, turn order) |
|---|---|
| 2 | west, east — facing each other |
| 3 | west, southeast, northeast — 120° apart, perfectly even |
| 4 | west, southwest, east, northeast — two opposing pairs |

Seat names are the compass direction of that player's home corner, which doubles as the camera
angle the **Focus** button returns to.

*(The published Kendo board has four fixed start corners, so its 3-player game is lopsided.
Ours is symmetric because the seats are generated from the board's own symmetry.)*

---

## 3. Architecture

```
┌─────────────────────────┐        ┌──────────────────────────────┐
│ Unity WebGL  (browser)  │  HTTP  │  Go server (single binary)   │      ┌────────────┐
│  – procedural board     │ ─────► │  /api/*  REST + polling      │ ───► │ PostgreSQL │
│  – polls game state     │        │  /       serves web/ (Unity) │      │ games+moves│
│ /dev HTML dev client    │        │  /dev    embedded dev client │      └────────────┘
└─────────────────────────┘        └──────────────────────────────┘
```

- The server is **authoritative**: clients send `{from, to}`; the server validates via DFS
  (exact path length, no revisit, no pass-through, king rules) and returns the new state,
  including the path it chose (for movement animation).
- Clients poll `GET /api/games/{id}?token=…&v={version}` every ~1.2 s; the server answers
  `204 No Content` when nothing changed.
- Board topology is served by `GET /api/board` — clients render whatever the server sends
  (single source of truth for geometry).

### 3.1 Repository layout

```
PROMPT.md                  ← this file
README.md                  ← run & deploy instructions
Dockerfile                 ← multi-stage: client bundle → Go build → tiny runtime image
docker-compose.yml         ← app + postgres, for local Docker use and reference
dev.ps1                    ← one-shot local dev (postgres + client build + server)
docs/spelregels.md         ← original Dutch rules
server/                    ← Go module "kingsreach"
  cmd/server/main.go
  internal/game/           ← pure rules engine + unit tests (no I/O)
  internal/store/          ← Store interface, Postgres (pgx) + memory implementations
  internal/httpapi/        ← HTTP handlers, profiles/catalog, static serving, /dev client
client/                    ← Babylon.js frontend (TypeScript)
  src/babylon.ts           ← single place Babylon is imported (deep imports = tree shaking)
  src/api.ts               ← typed REST client
  src/board.ts             ← board & piece views, highlights, move animation, graveyard
  src/seats.ts             ← seat table: colour, camera angle, display name
  src/skins.ts             ← unlockable piece skins (seat gives the hue, skin the finish)
  src/dice.ts              ← the opening roll, built from primitives
  src/environments.ts      ← unlockable 3D surroundings
  src/gltf.ts              ← lazily-loaded model loader (measured-bounds placement)
  src/ui.ts                ← HTML overlay (menu, lobby, seat list, collection, toasts)
  src/main.ts              ← controller: camera, input/drag, polling, game loop
  static/index.html        ← page shell + CSS
  build.mjs                ← esbuild bundle → ../web (+ pre-compressed .gz)
web/                       ← build output (generated, git-ignored)
.local/                    ← local-only tooling (portable PostgreSQL), not committed
```

### 3.2 REST API

| Method & path | Body / query | Result |
|---|---|---|
| `POST /api/games` | `{"mode":"online"\|"practice","players":2..4,"profile":…,"name":…,"password":…}` | `{state, token}` — creator takes the first seat; practice hands you the whole table |
| `POST /api/games/join` | `{"gameId":…,"password":…,"profile":…}` | `{state, token}` — the next free seat; the table starts (and rolls the dice) when it fills. `403` on a wrong password |
| `POST /api/games/{id}/start` | `{"token"}` | starts a table that never filled; the computer takes the empty seats |
| `GET /api/lobbies` | `?token=…` | `{lobbies:[…], running:[…]}` — see §3.5 |
| `GET /api/board` | – | board graph `{nodes:[{id,x,y,center}], edges:[{a,b,gold}]}` |
| `GET /api/config` | – | `{"steam":bool,"unlockAll":bool}` — which sign-in routes this server offers |
| `GET /api/geo` | – | `{"country":"NL"}` — the country guessed from the caller's address, `""` when unknown. Never an error |
| `GET /api/leaderboard` | `?token=…` | `{minWins, entries:[…], you, winsNeeded}` — see §3.6 |
| `GET /api/games/{id}` | `?token=…&v=N` | full state, or `204` if version still `N` |
| `GET /api/games/{id}/moves` | `?token=…&from=nodeId` | `{moves:[{to, path:[…]}]}` legal moves for that piece |
| `POST /api/games/{id}/move` | `{"token","from","to"}` | new state (or `409` + error for illegal moves) |
| `POST /api/games/{id}/roll` | `{"token","seat"?}` | state + `{rolledSeat, rolled}` — throws one die for that seat; `403` if it is not your throw |
| `POST /api/games/{id}/resign` | `{"token"}` | new state |
| `GET /healthz` | – | `{"ok":true,"store":"postgres"\|"memory"}` |

State payload (`you` is your seat, or `"all"` when one client runs the table):

```json
{
  "gameId":"…","code":"ABCDE","name":"Evening game",
  "mode":"online","status":"waiting|active|finished",
  "players":4,
  "seats":[{"seat":"west","skin":"clay","taken":true,"you":true,"out":false,
            "name":"Robo","avatar":"https://…","country":"NL"},
           {"seat":"southwest","skin":"royal","taken":true,"you":false,
            "out":true,"cause":"king-captured"}, …],
  "you":"west","turn":"east","winner":"","winReason":"",
  "phase":"play","version":7,"ply":6,"capturing":true,
  "dice":[[{"seat":"west","value":6},{"seat":"east","value":6}],
          [{"seat":"west","value":4},{"seat":"east","value":2}]],
  "pending":[],"yourRoll":false,
  "pieces":[{"id":"wK","owner":"west","value":1,"node":"-8,0","captured":false}, …],
  "lastMove":{"piece":"e2a","from":"5,-1","to":"4,0","path":["5,-1","4,0"],"captured":""}
}
```

`capturing` is false during the opening round. `dice` holds every round rolled, ties included.
Captured pieces stay in `pieces` with `captured:true` — that is what the client lays out beside
the board.

Errors: JSON `{"error":"…"}` with proper status codes (400 bad input, 403 wrong token/seat/password,
404 unknown table, 409 illegal move or wrong turn).

`code` is vestigial: tables are found in the lobby browser, not typed in.

### 3.3 Database schema (auto-migrated at startup)

```sql
CREATE TABLE IF NOT EXISTS games (
  id         text PRIMARY KEY,
  code       text UNIQUE NOT NULL,
  mode       text NOT NULL,
  status     text NOT NULL,
  players    integer NOT NULL DEFAULT 2,
  seats      jsonb NOT NULL DEFAULT '[]',  -- [{seat,token,profile,skin,taken,bot,name,avatar,country}]
  state      jsonb NOT NULL,
  version    integer NOT NULL,
  name          text NOT NULL DEFAULT '',  -- table name in the lobby browser
  password_hash text NOT NULL DEFAULT '',  -- sha256(salt \0 password); empty = open
  password_salt text NOT NULL DEFAULT '',
  host_name     text NOT NULL DEFAULT '',
  host_country  text NOT NULL DEFAULT '',  -- ISO-3166 alpha-2, for the flag
  house      boolean NOT NULL DEFAULT false, -- opened by the server, not a player
  listed     boolean NOT NULL DEFAULT true,  -- appears in the browser
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS profiles (
  id    text PRIMARY KEY,
  token text UNIQUE NOT NULL,
  kind  text NOT NULL DEFAULT 'guest',      -- 'steam' | 'guest'
  name text NOT NULL DEFAULT '', avatar text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '', steam_id text UNIQUE,
  games_played integer NOT NULL DEFAULT 0, wins integer NOT NULL DEFAULT 0,
  equipped_skin text NOT NULL DEFAULT 'clay',
  equipped_env  text NOT NULL DEFAULT 'picnic',
  created_at timestamptz NOT NULL DEFAULT now()
);
-- token_west/token_east/profile_*/skin_* predate `seats` and are no longer
-- written. Rows created before the change are rebuilt into seats on read, so
-- the columns stay (with defaults, or their NOT NULL breaks new inserts).
CREATE TABLE IF NOT EXISTS moves (
  id bigserial PRIMARY KEY,
  game_id text NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  ply integer NOT NULL,
  piece text NOT NULL,
  from_node text NOT NULL,
  to_node text NOT NULL,
  path jsonb NOT NULL,
  captured text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS moves_game_idx ON moves(game_id, ply);
```

Config via env: `PORT` (default 8080), `DATABASE_URL` (empty ⇒ in-memory store, warn loudly),
`STATIC_DIR` (default `web`), `PUBLIC_URL` (the address players reach the game on — required for
Steam sign-in, which is disabled without it), `KINGSREACH_UNLOCK_ALL=1` (dev: everything unlocked),
`KINGSREACH_BOT_LOBBIES=0` (turn off the server's own tables), `GEOIP_URL` (country lookup, see
§3.4), `TRUST_PROXY=1` (believe `X-Forwarded-For` — set this only when something really is in
front). Single-replica deployment assumed; per-game mutex in the API layer serializes writes.

### 3.4 Identity

Two ways in, chosen on the first screen:

| | Steam | Guest |
|---|---|---|
| How | OpenID 2.0 `checkid_setup`, callback verified with `check_authentication`; name and avatar from the public profile XML (no API key) | type a name, optionally pick a country |
| Progress | kept — `BumpProfileStats` writes only `WHERE kind = 'steam'` | none, by design |
| Returns as | `/#steam=<token>` (a fragment never reaches a server log or a `Referer`); the client reads it once and wipes it | a token in `localStorage` |

`claimed_id` is matched against `steamcommunity.com` **before** verification, so a forged identity
URL never even reaches Steam. Display names are stripped of control characters and capped at 24
runes; countries must be a bare ISO-3166 alpha-2 code.

**Country guess** (`geoip.go`). Three sources, cheapest and most private first:

1. A country header from whatever sits in front of us — `CF-IPCountry` and friends. Free, instant,
   nothing leaves the building, and the only source that also covers IPv6. Cloudflare's `XX`
   (unknown) and `T1` (Tor) are not countries and are ignored.
2. **A table compiled into the binary** (`internal/geoip`). No network, no third party, no rate
   limit, ~37 ns per lookup. IPv4 only — that is the dataset's limit, not an oversight.
3. `GEOIP_URL` (must contain `{ip}`), if set. This hands the player's address to a third party, so
   it stays **off unless configured**, and now exists mainly to cover the IPv6 addresses step 2
   cannot answer. Results are cached per address for 12 hours, misses included, so a dead service
   is asked once rather than on every visit. 2-second timeout.

Loopback, private, link-local and unparseable addresses are never looked up. `X-Forwarded-For` is
only believed when `TRUST_PROXY` is set — anyone can send that header, and behind no proxy it is
pure attacker input. The result **preselects the picker** on the sign-in screen; the player sees
it and can change it before it reaches their profile. It also fills the country on a fresh Steam
profile, since Steam's public profile carries no country without an API key.

The table is built from [datasets/geoip2-ipv4](https://github.com/datasets/geoip2-ipv4), which
refreshes weekly from MaxMind's GeoLite2 Country database. `cmd/geoipgen` reduces the 30 MB CSV to
a 2.1 MB file: sorted *boundaries* rather than ranges (the end of one block is the start of the
next), each six bytes — a `uint32` address and the country packed into a `uint16`. Runs of one
country collapse; gaps get an explicit "nobody" marker so a lookup inside unallocated space cannot
inherit the block before it. Refreshing is: download the CSV, `go run ./cmd/geoipgen`, commit the
`.bin`. **MaxMind's terms require the credit**, which is why it sits in `client/src/credits.ts`
alongside the artwork.

A caution for tests: a geo database answers about *registration*, not about who operates an
address. `1.1.1.1` is Cloudflare's resolver and resolves to **AU**, because APNIC holds the block.

### 3.5 The lobby browser — "Populated browser"

`GET /api/lobbies` returns two lists:

- **`lobbies`** — real, joinable tables: `{gameId, name, host, country, players, taken, locked, age, yours}`.
  Between one and four of these are opened by the server itself when there are not enough
  player-made ones (`house = true`), each hosted by a computer player. They are ordinary rows in
  `games`: anyone can join one, and the computer then plays the other side (`RunBots`, one action
  per 900 ms tick — throws first, then moves). A table nobody joins is swept after 30 minutes.
- **`running`** — tables already under way: `{name, host, country, players, ply, minutes}`.
  Real active online games first, then padded so the room never looks abandoned. **The padded
  entries are presentation only**: never stored, never joinable, never counted anywhere. Their
  seed is a 90-second time bucket, so the list stays put between polls instead of flickering.

Practice games and games untouched for ten minutes are left out of `running`; games abandoned for
six hours are deleted outright. No handle and no table name appears twice in one response —
across *both* lists, and comparing names by `nameKey` so "tem alguem ai" and "tem alguem ai!!"
count as one. A player at two tables at once is the tell that gives a generator away.

**Hosts and table names** (`names.go`). Handles are built, not listed: a fixed roster repeats
itself inside one screenful of a busy room. ~45 prefixes × ~50 cores across eleven weighted
styles — `FrostRaven`, `frost_raven`, `frostraven99`, `marijke92`, `bram_nl`, `fr05tr4v3n`,
`xX_FrostRaven_Xx` — gives a pool nobody plays through. The weights matter: `xX_…_Xx` is
unmistakable, so at one style in eleven it stops reading as one player's taste and starts reading
as a template.

Each host names their table **in their own language**, drawn from `tableNamesByLang` and written
in the register people actually use (mostly lowercase, often a question). `humanise` then roughens
it slightly — drops a question mark, adds `!!` or ` :)`, and once in twenty shouts it. All three
are long shots, so most names come through untouched; a room where every third table is SHOUTING
reads as generated, which is the opposite of the point. Turkish and Japanese are never
upper-cased — Japanese has no case, and Go's `ToUpper` turns Turkish "i" into "I" rather than "İ".
`names_test.go` measures these rates rather than trusting them.

Passwords are salted SHA-256 (`sha256(salt \0 password)`). A locked table still shows in the
browser with `locked: true` — you can see it exists, you just cannot sit down without the word.

### 3.6 Standings

Twenty victories (`LeaderboardMinWins`) buy a place in the hall of champions. Below that a player
is **absent** — not ranked low, not greyed out — which is what makes arriving on it mean anything.
`winsNeeded` tells them the distance instead. Guests can never be ranked and are never given a
target, since they keep no progress to rank.

`awardStats` decides what counts, and two of its rules were bugs found in play:

- A win needs a genuine opponent: another profile **or the computer**. The original rule demanded
  two human profiles, so every victory over the AI was silently dropped.
- The computer finishes plenty of games itself — by winning, or by walling the last rival in — so
  `stepOneGame` has to award stats too. Only the human move path did, which meant a game lost to
  the AI was not even recorded as played.

Self-play is still worth nothing: one profile in two seats is not a contest. Offline records
nothing at all.

### 3.7 Engine invariants (unit-tested)

- Board: 55 nodes, 78 edges, exactly 6 gold edges, Throne degree 6, kings start at `±8,0`.
- Exact-step DFS: a 2 can never "bounce" back to its start; blocked first steps ⇒ no moves.
- Non-kings never reach the Throne nor traverse gold edges; the King (and only the King) can.
- Reaching the Throne ends the game at once; capturing a King only knocks that player out.
- Rotating any node by 60° six times returns it home — the symmetry every seat is built on.
- Four seats place 32 pieces on 32 distinct fields, and all four can move at setup.
- Turn order travels round the table and wraps; knocked-out seats are skipped.
- A knocked-out player's pieces stay on the board and can never be moved again.
- Strikes are refused (with `ErrNoCaptures`) and not offered during the opening round.
- Stalemate (`HasAnyLegalMove == false`) knocks that player out; the last player in wins.
- A new game starts in the `roll` phase; moving before the dice settle gives `ErrStillRolling`.
- A seat may throw once per round (`ErrNotYourRoll` on a second attempt), a tie sends exactly
  the tied seats back to the dice, and the highest re-roll takes the opening move.

---

## 4. Frontend spec (Babylon.js)

- **Everything procedural.** Board, pieces, skins and environments are built from Babylon
  primitives and runtime-generated materials/textures — no meshes, no images, no art pipeline.
  The whole page is `index.html` + one bundled script.
- **Visual theme — match the physical set:** dark cloth hexagon, teal embroidery lines, gold
  Gilded Paths and Throne, stones with pips (1 centre, 2 side-by-side, 3 in a triangle, like the
  photo). A `GlowLayer` gives the gold and the lit fields their bloom.
- **Colour is the seat's, finish is the skin's** (`seats.ts` × `skins.ts`). A seat supplies one
  hue — Obsidian, Jade, Ember, Azure… — and the skin decides whether it appears as matte
  ceramic, polished metal, a glowing gem or carved stone. That split is why every skin works at
  a four-player table without a palette per combination. Pip colour is picked by luminance so
  the value stays readable on light and dark stones alike.
- **Screens (HTML overlay, not in-canvas):** Sign-in (Steam, or a guest name + country) → Menu
  (Find a table / Open your own table / Practice / Collection / Resume) → Board room (a scrollable
  table with a sticky header: search across name/host/country, a live count, refresh, an
  open/in-progress/all filter and sortable columns; flags, handles, seats filled and a lock where
  a password is needed. Joinable tables always sort above games under way) → Open-a-table (name, 2/3/4 seats, optional password) → Lobby (who is seated, and *Start now*,
  which hands the empty seats to the computer) → Game (status bar; seat list down the left with
  flag, name, turn marker and knock-out reason; bottom bar with **Focus**, Resign, Leave)
  → result overlay.
- **Country flags are drawn in CSS** (`flags.ts`), not emoji. Windows ships no glyphs for regional
  indicator pairs, so 🇳🇱 renders there as the bare letters "NL"; layered gradients look the same
  everywhere. Unknown codes fall back to a neutral chip with the two letters.
- **Music** (`music.ts`): a `TRACKS` array is the whole configuration — drop a file in
  `client/static/audio/` and add a line. `preload="none"` and the `src` is only assigned when
  playback actually starts, so a player who mutes never downloads a byte; the Go server answers
  range requests, so the track streams instead of arriving in one lump. Browsers refuse audio
  until a user gesture, so the first click anywhere starts it. Mute persists in `localStorage`.
- **Credits are data, not prose** (`credits.ts` + the `licence` fields in `music.ts`). Every
  borrowed asset here is Creative Commons Attribution, and CC-BY requires the credit to reach the
  people *using* the work — a line in a README nobody opens does not discharge it. So the
  Collection screen renders the credits from the same arrays that declare the assets, and they
  cannot drift out of sync with what actually ships. A complete credit is four things: title,
  author, licence, source. Adding an asset means adding a row.
- **Strikes look and behave differently from ordinary moves.** `GET .../moves` returns `captures`
  (the victim's piece id) alongside each destination, so the client can mark a strike with a red
  ring *around* the stone rather than a teal disc under it. Two details make it legible: the ring
  is excluded from the `GlowLayer`, whose bloom washes strong colours to white, and its material
  is `unlit` — a lit torus blows out where the light hits it, which is fine for a stone and
  useless for a warning. Clicking the enemy stone captures it: the piece sits on top of its own
  marker, so the pick never reaches the ring underneath, and without that case a capture could
  only be made by dragging.
- **Opening roll:** the die (`client/static/models/dice.glb`, ~89 KB) hovers over the board
  turning gently until the player throws it — by clicking it or the *Throw the die* button. Each
  seat throws its own; ties send only the tied seats back. Driven entirely by the server's
  `phase`/`pending`/`yourRoll` fields, so a reload mid-roll picks up exactly where it was.
  **The `FACE_UP` table in `dice.ts` was measured, not guessed** — all six candidate rotations
  were rendered side by side and read off, and this model numbers its Z faces the opposite way
  round from the obvious assumption (3 and 4 are swapped). Re-measure if the model is replaced.
- **Graveyard:** struck stones are laid on small slabs just outside the board on their *owner's*
  side, four to a row, stepping outward. They are unpickable scenery, so they can never be
  mistaken for a move target.
- **Interaction:**
  - press a stone of yours → server-provided legal destinations light up (pulsing discs);
  - drag it → the stone lifts, the destination under it swells; release to move, release
    elsewhere to put it back;
  - or click stone → click a lit field (identical result, better for touch);
  - drag anywhere else orbits an `ArcRotateCamera` (β clamped 0.22–1.45), wheel zooms,
    **Focus** tweens back to your own seat's angle (from `seats.ts`; in practice mode: whoever is
    to move). Camera control is detached while a stone is held, tracked by a flag so it is never
    attached twice.
  - Opponent moves arrive by polling and hop node-by-node along the server's path.
- **Robustness rules learned the hard way:**
  - state application is queued and the animation flag is cleared in a `finally` — a stuck flag
    freezes the HUD *and* polling;
  - tweens carry a wall-clock fallback, because a hidden tab produces no frames and would
    otherwise never settle the promise;
  - polling failures are logged, never swallowed silently.
- **Build:** `npm run build` in `client/` (esbuild → `web/kingsreach.js` + `.gz`, ~1.6 MB /
  ~380 KB). `npm run watch` for development, `npm run typecheck` for strict TS checking.
  The Go server serves the pre-compressed file to clients that accept gzip.
- **Debug handle:** `window.kingsreach` exposes the controller (plus `screenOf(nodeId)` for
  projecting a board field to screen space) — used by the browser end-to-end checks.

---

## 5. Deployment (Easypanel)

1. Push the repo to Git (`web/` is build output; the Dockerfile builds it).
2. Easypanel → create a **Postgres** service (e.g. `kingsreach-db`).
3. Easypanel → create an **App** service from the repo, build type **Dockerfile**.
4. Env vars: `DATABASE_URL=postgres://user:pass@kingsreach-db:5432/kingsreach?sslmode=disable`
   (internal hostname = the Postgres service name), optionally `PORT` (default 8080).
5. Expose port 8080, attach a domain. Done — the same container serves the game and the API.

`docker-compose.yml` mirrors this for local Docker testing (`docker compose up --build`).

---

## 6. Local development (no Docker required)

- `dev.ps1` starts everything (postgres + client build + server). Manually:
- `go -C server test ./...` — engine + API tests. `npm --prefix client run typecheck` — strict TS.
- Portable PostgreSQL lives in `.local/pgsql`; data dir `.local/pgdata`, port **5433**
  (see README for the exact init/start commands).
- `DATABASE_URL=postgres://kingsreach:kingsreach@localhost:5433/kingsreach?sslmode=disable go -C server run ./cmd/server`
- Open `http://localhost:8080` — the 3D client if `web/` is built, otherwise a landing page;
  `http://localhost:8080/dev` — the embedded 2D fallback client (fully playable).

---

## 7. Progression & unlockables

- **Profiles & progression (no login)**: the client stores a secret profile token in
  `localStorage`. The server tracks `gamesPlayed`/`wins`; **only online games** advance stats
  (practice counts one played game, never a win; self-play never awards wins). This is the
  natural seam to add real accounts later.
- **Unlockables** (server-authoritative catalog, `GET /api/catalog`):

  | Kind | Id | Name | Requirement |
  |---|---|---|---|
  | skin | `clay` | Clay Buttons | free (default) |
  | skin | `royal` | Royal Gold | 1 win |
  | skin | `crystal` | Crystal Court | 3 wins |
  | skin | `rune` | Runestones | 5 games played |
  | env | `picnic` | Picnic Meadow | free (default) |
  | env | `fair` | Medieval Fair | 2 games played |
  | env | `dust2` | Bombsite B | 2 wins |
  | env | `store` | Boardgame Store | 4 wins |
  | env | `cafe` | Streetside Café | 8 games played |

  Skins restyle your pieces (each has west/east variants; the opponent sees your skin —
  it is snapshotted onto the game at create/join as `westSkin`/`eastSkin` in the state).
  Environments are the 3D scenery around the board and are purely local to each viewer.
- **Profile API**: `POST /api/profile` → `{token, gamesPlayed, wins, equippedSkin, equippedEnv,
  unlocked[]}` · `GET /api/profile?token=…` · `POST /api/profile/equip {token, skin?, env?}`
  (409 when locked). `POST /api/games` / `join` accept an optional `"profile"` token.
- **DB**: `profiles` table + `games.profile_west/profile_east/skin_west/skin_east`
  (auto-migrated).
- **Dev mode**: `KINGSREACH_UNLOCK_ALL=1` reports every catalog item as unlocked and lets any
  profile equip anything. Enforcement stays server-side either way — the client never decides
  what is unlocked — and stats keep accruing normally, so turning the flag off restores the
  real gates.
- **Imported environments**: `client/static/models/*.glb`, loaded on demand (the glTF loader is
  reached through a dynamic import). `loadGlb` places a model by *measured bounds*, never by
  hardcoded units: give it a target width, an optional `zUp` for Source/CAD exports, and an
  `anchor` — a point in the model's own upright, unscaled frame that should land at a world
  position. That is how the board ends up standing exactly on one crate in a whole game map,
  and why the scale and the anchor can be tuned independently.

## 8. Appendix — the Unity frontend (superseded, kept in `unity/`)

Only relevant if someone revives it. Hard-won build lessons:

- Never add built-in shaders to *Always Included Shaders* from editor code — `Shader.Find` can
  return a shader living inside `unity_builtin_extra`, and that self-reference makes **every**
  player build fail with `Assertion failed: m_LockCount == 0` while writing that same file.
  It is stored in `ProjectSettings/GraphicsSettings.asset`, so clearing `Library/` does not help.
  Use reference materials in `Assets/Resources/` instead.
- `Assets/link.xml` preserves the collider classes `GameObject.CreatePrimitive` adds at runtime;
  `PlayerSettings.stripEngineCode = false` covers the rest for a fully code-built scene.
- Batch builds: `-batchmode -buildTarget WebGL -executeMethod WebGLBuilder.BuildCli`, **without**
  `-quit` (it races the build pipeline; `BuildCli` exits with its own status code).

## 9. Acceptance checklist

- [x] `go test ./...` green (engine invariants above).
- [x] `npm run typecheck` green (strict TypeScript, no unused locals).
- [x] Create practice game in the browser; both sides playable; legal-move hints correct.
- [x] Drag a stone: it lifts, legal fields light up, dropping commits the server-validated move.
- [x] Orbit by dragging the background; **Focus** returns to the player's side.
- [x] Two browser tabs: create + join an online game with the code; moves sync via polling.
- [x] Captures work; capturing the King ends the game; King → Throne ends the game.
- [x] Blocked player loses immediately; resign works.
- [x] Games and moves are rows in Postgres; server restart resumes games from the DB.
- [x] Winning online unlocks skins/environments; equipping applies and persists; locked items
      are refused by the server (409).
- [ ] `docker build` succeeds; container + Postgres via compose serve the same experience.
      *(Not verified here — Docker is not installed on this machine.)*
