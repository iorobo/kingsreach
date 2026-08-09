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

The bundle is served under a content hash (`kingsreach.js?v=…`) so a new release can never be
answered with a cached copy of the last one, while `index.html` itself is always revalidated.
The Go server serves the pre-compressed file automatically. A minimal 2D fallback client stays embedded in the binary at `/dev` — handy for
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

A stone makes a soft knock as it lands and a sharper crack when it takes something, each with a
ring on the cloth. Both follow the ♪ mute — nobody means "silence the music but keep the clicks".

**You have 150 seconds** to roll or move. The countdown sits in the top bar on your turn and turns
red near the end; run it out and you forfeit your seat, and the game goes to whoever is left — the
computer included. Offline games have no clock. Change it with `KINGSREACH_MOVE_SECONDS`.

Once somebody's time is actually up, the seat list offers a **Boot** button to whoever is still
playing. It does nothing the clock sweeper would not have done a second later, and refuses while
their clock is still running — but staring at an expired countdown with no button is
indistinguishable from the server having forgotten about you.

## Colours, and sides

**Pick the colour you want to play** in the Collection screen. It used to be the seat's: west was
Obsidian and that was that, which made "I want to play green" really mean "I want the south-west
chair". The seat still decides where you sit and where the camera looks from; the colour is yours.

If two players want the same one, **whoever wins the opening throw gets it** and the other falls
back to their seat's own colour. Using the dice for this was not a shortcut — it is the answer the
game already gives to every other contested question, and it means the tie-break happens at the
table where everyone can watch it. Colours are handed out once, when the throws settle, and never
change again mid-game.

**Team games** need four seats. Partners sit opposite each other, which also makes the turn order
alternate between the sides — you never get two of one team in a row. Two options ride along:

| Option | Off (default) | On |
|---|---|---|
| Friendly fire | you cannot strike your partner | you can |
| Fallen partner's stones | stay on the board as obstacles | you move them on your own turn |

A team game ends when one **side** is left standing rather than one player, so it can finish with
two players still on the board.

## Friends and invitations

The friend list comes from **Steam**, and shows the friends who have signed in to Kingsreach at
least once. There is deliberately no friend list of our own: building one means a request flow, an
accept flow, a block flow and a report flow, all to arrive at a worse copy of the list the player
already has. Filtering to people who have played here is the part that makes it useful — you
cannot invite somebody who has no seat to sit in.

Two things it needs, and it says so on screen when either is missing:

- **`KINGSREACH_STEAM_KEY`**, a Steam Web API key ([get one here](https://steamcommunity.com/dev/apikey)).
  `GetFriendList` will not answer without one, unlike the public profile XML the sign-in reads for
  names and avatars.
- **A public Steam friend list.** Steam answers 401 for private ones, which is the player's setting
  to change rather than a bug to work around.

From a table with a free seat, **Invite a friend** puts an invitation in their menu. It is a
database row and nothing more — the client is already polling, so it asks for its invitations the
same way it asks for everything else, and an invitation to a table that has since filled up or
been swept simply stops existing.

## Several games at once

The menu lists **every table you hold a seat at**, marks the ones waiting on you, and takes you
back into any of them. The seat token comes with each row, so a closed tab is not a lost game: a
seat is remembered against your account, and walking back in hands you the same one.

The computer plays at **easy, medium or hard**, picked at random when it sits down, and pauses
before moving so it reads as an opponent rather than a script. Easy scores the position after its
own move and stops; medium adds your best answer to that; hard searches four plies — two moves for
each side — with alpha–beta and captures ordered first so the pruning pays for the depth. Over ten
games the levels score hard 10–0 easy, medium 9–1 easy and **hard 9–1 medium**, which is the number
that says the extra depth is worth something rather than just costing time (`go test
./internal/game -run TestHardBeatsMedium -v`). Easy still takes the odd bad move and sometimes
misses a capture outright. Beating any of them counts towards the hall of champions.

Given the same seed the bot now plays the same game twice. That is not free — move generation
returns a map, and Go randomises map iteration, so the destinations are sorted before the random
tiebreak draws its numbers. Worth the few string compares: a bot that answers differently on
identical input cannot be debugged, and a strength test measuring it means nothing.

**Winning** drops a gold crown onto your king in a shower of sparks and plays a fanfare over the
background music. **Losing** gets its own ending rather than the same box with a different word in
it: the board darkens and a tarnished crown topples onto your square. The loser's version does not
touch the king piece, deliberately — the usual way to lose *is* the king being taken, so by the
time it plays there is nothing standing on that square to knock over. Both live in
`client/src/finale.ts`; the fanfare follows the ♪ mute, because someone who turned the music off
did not mean "except when I win".

The result itself arrives as a **bar along the bottom** rather than a card across the middle — a
dialog would cover the ending it is there to announce. It takes the bottom bar's place while it is
up, and carries Focus so you can still swing the camera back to your own side of the table.

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
   | `KINGSREACH_STEAM_KEY` | unset | Steam Web API key. Without it the friend list is unavailable and says so. |
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
or country, filter to open or in-progress games, and sort by name, seats or age.

To keep the room from looking abandoned, the server keeps a couple of tables of its own open and
runs a handful of **exhibition matches between its own computer players**. Those are real games —
you can press *Watch* and follow one move by move, which is also the easiest way to see how the
three difficulty levels actually play. Set `KINGSREACH_BOT_LOBBIES=0` to switch all of it off.

They are started **one at a time, 25–70 seconds apart, from the same ticker that takes the bot
turns** — rather than all at once, the moment somebody first opens the room. That is the difference
between a list of games all on move one and a list worth opening: after a few minutes the room
holds a match just beginning next to one deep into its endgame. An exhibition advances a ply per
tick, so it passes fifty within the minute.

Anything in progress can be watched, including games between people. A spectator holds no seat, so
they can see the board and nothing else.

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
`POST /api/games/{id}/resign` · `POST /api/games/{id}/rematch` · `GET /api/games/{id}/watch?v` ·
`GET /api/leaderboard?token` · `GET /api/catalog` ·
`POST /api/profile` `{name, country}` ·
`GET /api/profile?token` · `POST /api/profile/update` · `POST /api/profile/equip` ·
`GET /api/auth/steam/login` · `GET /api/auth/steam/return` · `GET /healthz`.

## Taunts

The 💬 button calls a line out to the table: nine recorded phrases, from *Nice
move* to *Bow now, save yourself the trouble later*. Every one also appears as a
speech bubble in the corner, because plenty of people play with the sound off
and a taunt nobody can hear is not a taunt.

Two limits, both enforced on the server rather than by greying out the button —
a disabled button has never stopped anybody determined. Eight seconds between
taunts, and twelve per game: the gap alone would still allow one every eight
seconds for an entire match.

Add a line by dropping the audio in `client/static/audio/taunts/` and adding a
row to `taunts` in `server/internal/httpapi/taunts.go`. **The text must be what
the voice actually says** — it is what somebody with the sound off reads
instead, so a mismatch is two different taunts wearing one name.

## How to play

The menu has a **How to play** screen with the rules and diagrams. The diagrams
are drawn from the board the server sends, not from hand-made copies, so an
illustration cannot quietly drift away from the game it illustrates.

## Changelog

The version sits at the bottom of the menu; click it to see what changed. Entries live in
`client/src/changelog.ts` — add a release at the top and the game reports that version. It is
deliberately *not* generated from the git log: commit messages are written for whoever maintains
the code, and a changelog is written for whoever plays the game.

## Repository layout

```
client/    Babylon.js frontend (TypeScript, esbuild → web/)
  static/models/   imported .glb scenery, loaded on demand
server/    Go module: rules engine, REST API, static hosting, embedded /dev client
web/       build output (generated — not committed)
docs/      original Dutch rules
```

### Third-party assets

Almost everything borrowed here is **Creative Commons Attribution**, which means the credit is a
condition of use, not a nicety — so it is shown in the game's Collection screen, rendered from
`client/src/credits.ts` and `client/src/music.ts` rather than hard-coded. Add an asset, add a row.

| Asset | Work | By | Licence |
|---|---|---|---|
| `client/static/models/de-dust2.glb` | [de_dust2 - CS map](https://sketchfab.com/3d-models/de-dust2-cs-map-056008d59eb849a29c0ab6884c0c3d87) | pancakesbassoondonut (Sketchfab) | CC BY 4.0 |
| `client/static/models/dice.glb` | [Dice](https://sketchfab.com/3d-models/dice-3b955af797e140eca0947ede57f412ba) | tnRaro (Sketchfab) | CC BY 4.0 |
| `client/static/models/king-crown.glb` | [King crown](https://sketchfab.com/3d-models/king-crown-909b3f198d5b49cea3f68549a8f57b51) | marekc (Sketchfab) | CC BY 4.0 |
| `client/static/audio/magic-escape-room.mp3` | Magic Escape Room | Kevin MacLeod (incompetech.com) | CC BY 4.0 |
| `client/static/audio/victory.mp3` | [Medieval: The Old Tower Inn](https://opengameart.org/content/medieval-the-old-tower-inn) | RandomMind (OpenGameArt) | CC0 |
| `server/internal/geoip/ipv4-country.bin` | GeoLite2 Country data, via [datasets/geoip2-ipv4](https://github.com/datasets/geoip2-ipv4) | MaxMind | GeoLite2 EULA — credit required |

The victory fanfare is the exception: CC0 asks for nothing at all. It is credited anyway, because
that list exists to record where the game's borrowed parts came from, not only where a licence
compels it.

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
