package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"
	mrand "math/rand"
	"net/http"
	"time"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

// The lobby browser, and the computer players that sit in it.

type lobbyEntry struct {
	GameID  string `json:"gameId"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Country string `json:"country"`
	Players int    `json:"players"` // seats at the table
	Taken   int    `json:"taken"`   // seats filled
	Locked  bool   `json:"locked"`  // needs a password
	Age     int    `json:"age"`     // seconds since it opened
	Yours   bool   `json:"yours"`   // you already hold a seat here
}

// runningEntry is a table already under way. Games in progress cannot be
// joined, so these are display-only.
type runningEntry struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Country string `json:"country"`
	Players int    `json:"players"`
	Ply     int    `json:"ply"`
	Minutes int    `json:"minutes"`
}

func (s *Server) handleLobbies(w http.ResponseWriter, r *http.Request) {
	s.seedLobbies(r.Context())

	// The client sends its *profile* token here, not a seat token — those are
	// different secrets, and comparing them meant "yours" never lit up.
	myProfile := ""
	if token := r.URL.Query().Get("token"); token != "" {
		if p, err := s.st.GetProfileByToken(r.Context(), token); err == nil {
			myProfile = p.ID
		}
	}
	games, err := s.st.ListOpen(r.Context(), 40)
	if err != nil {
		logf("lobby list failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not list tables")
		return
	}
	out := struct {
		Lobbies []lobbyEntry   `json:"lobbies"`
		Running []runningEntry `json:"running"`
	}{Lobbies: []lobbyEntry{}, Running: []runningEntry{}}
	// Hosts and names already on screen, so the running list never repeats one.
	seenHost, seenName := map[string]bool{}, map[string]bool{}
	for _, g := range games {
		_, mine := g.SeatForProfile(myProfile)
		taken := len(g.Seats) - g.FreeSeats()
		out.Lobbies = append(out.Lobbies, lobbyEntry{
			GameID: g.ID, Name: g.Name, Host: g.HostName, Country: g.HostCountry,
			Players: g.Players, Taken: taken, Locked: g.Locked(),
			Age: int(time.Since(g.CreatedAt).Seconds()), Yours: mine,
		})
		seenHost[g.HostName], seenName[nameKey(g.Name)] = true, true
	}
	out.Running = s.runningTables(r.Context(), seenHost, seenName)
	writeJSON(w, http.StatusOK, out)
}

// runningTables lists games under way. Real ones first; the rest are filled in
// so the board room never looks abandoned. See PROMPT.md §3.5 "Populated browser"
// — the padded entries are presentation only: they are never stored, never
// joinable, and never counted anywhere.
// seenHost and seenName carry the entries already shown in the open list, so
// the same person never turns up twice in the same room.
func (s *Server) runningTables(ctx context.Context, seenHost, seenName map[string]bool) []runningEntry {
	out := []runningEntry{}
	if games, err := s.st.ListActive(ctx, 40); err == nil {
		for _, g := range games {
			// A practice board is one player against themselves, and a game
			// nobody has touched in ten minutes has been walked away from.
			// Neither belongs on a list of games you could be watching.
			if g.Mode != ModeOnline || time.Since(g.UpdatedAt) > 10*time.Minute {
				continue
			}
			if seenHost[g.HostName] || seenName[nameKey(g.Name)] {
				continue // already on screen in the open list
			}
			host := g.HostName
			if host == "" {
				host = "A player"
			}
			seenHost[g.HostName], seenName[nameKey(g.Name)] = true, true
			out = append(out, runningEntry{
				Name: g.Name, Host: host, Country: g.HostCountry, Players: g.Players,
				Ply: g.State.Ply, Minutes: int(time.Since(g.CreatedAt).Minutes()),
			})
			if len(out) == 6 {
				break
			}
		}
	}
	if !s.BotLobbies {
		return out
	}
	// Pad with a slowly drifting set. The seed is a coarse time bucket so the
	// list stays put between polls instead of flickering.
	bucket := time.Now().Unix() / 90
	rng := mrand.New(mrand.NewSource(bucket))
	want := 1 + rng.Intn(8) // one to eight games under way

	// One table per host, and no two tables with the same name. A room where
	// Yuki is playing twice under an identical title reads as a generator, not
	// as a room. Bail out rather than spin if the pools run dry.
	for _, e := range out {
		seenHost[e.Host] = true
		seenName[nameKey(e.Name)] = true
	}
	for attempts := 0; len(out) < want && attempts < want*40; attempts++ {
		host := randomHost(rng.Intn)
		if seenHost[host.Name] {
			continue
		}
		name := tableNameFor(host, rng.Intn)
		if seenName[nameKey(name)] {
			continue
		}
		players := 2
		if rng.Intn(4) == 0 {
			players = 3 + rng.Intn(2)
		}
		seenHost[host.Name], seenName[nameKey(name)] = true, true
		out = append(out, runningEntry{
			Name:    name,
			Host:    host.Name,
			Country: host.Country,
			Players: players,
			Ply:     4 + rng.Intn(60),
			Minutes: 1 + rng.Intn(25),
		})
	}
	return out
}

// ---- computer players ----
//
// The host pool and the table names they use live in names.go.

// cryptoPick draws from the same source as the tokens, for tables that really
// exist.
func cryptoPick(n int) int { return randRange(0, n-1) }

func pick[T any](items []T) T {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(items))))
	if err != nil {
		panic(err)
	}
	return items[n.Int64()]
}

func randRange(lo, hi int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(hi-lo+1)))
	if err != nil {
		panic(err)
	}
	return lo + int(n.Int64())
}

// seedLobbies keeps a handful of open tables around so the browser is never
// empty. Each is hosted by a computer player that takes its turns once a
// human sits down.
func (s *Server) seedLobbies(ctx context.Context) {
	if !s.BotLobbies {
		return
	}
	s.seedMu.Lock()
	defer s.seedMu.Unlock()
	if time.Since(s.lastSeed) < 4*time.Second {
		return
	}
	s.lastSeed = time.Now()

	if n, err := s.st.Sweep(ctx, 30*time.Minute, 6*time.Hour); err != nil {
		logf("lobby sweep failed: %v", err)
	} else if n > 0 {
		logf("swept %d stale tables", n)
	}

	open, err := s.st.ListOpen(ctx, 40)
	if err != nil {
		return
	}
	live := 0
	taken := map[string]bool{}
	for _, g := range open {
		taken[g.HostName] = true
		if g.House {
			live++
		}
	}
	// Someone already at a table cannot also be sitting at a new one. Without
	// this, a host whose game is still running gets handed a second table.
	if active, err := s.st.ListActive(ctx, 40); err == nil {
		for _, g := range active {
			taken[g.HostName] = true
			for _, seat := range g.Seats {
				if seat.Bot {
					taken[seat.Name] = true
				}
			}
		}
	}
	// Re-roll the target every pass so the count drifts the way a real
	// browser would rather than sitting on a constant.
	if s.seedTarget == 0 || live == 0 {
		s.seedTarget = randRange(1, 2)
	}
	for live < s.seedTarget {
		host, ok := freeHost(taken)
		if !ok {
			return // everyone is already hosting something
		}
		if err := s.createBotLobby(ctx, host); err != nil {
			logf("could not open a table: %v", err)
			return
		}
		taken[host.Name] = true
		live++
	}
}

// freeHost invents a player who is not already at a table, so the same handle
// never appears twice in the room. Handles are generated, so the pool never
// runs dry — the attempt cap is only there to stop a pathological loop.
func freeHost(taken map[string]bool) (botHost, bool) {
	for i := 0; i < 50; i++ {
		h := randomHost(cryptoPick)
		if !taken[h.Name] {
			return h, true
		}
	}
	return botHost{}, false
}

func (s *Server) createBotLobby(ctx context.Context, host botHost) error {
	players := 2
	if randRange(1, 4) == 1 {
		players = randRange(3, 4)
	}
	seatOrder := game.SeatOrder(players)
	rec := &store.GameRecord{
		ID: randHex(12), Mode: ModeOnline, Status: "waiting", Players: players,
		State:   game.NewState(seatOrder, seatOrder[0]),
		Version: 1,
		Name:    tableNameFor(host, cryptoPick), HostName: host.Name, HostCountry: host.Country,
		House: true, Listed: true,
	}
	for i, seat := range seatOrder {
		st := store.Seat{Seat: seat, Skin: DefaultSkin}
		if i == 0 {
			st.Taken, st.Bot = true, true
			st.Token = randHex(16)
			st.Name, st.Country = host.Name, host.Country
		}
		rec.Seats = append(rec.Seats, st)
	}
	var err error
	for i := 0; i < 6; i++ {
		rec.Code = randCode()
		if err = s.st.Create(ctx, rec); err == nil {
			return nil
		}
	}
	return err
}

// fillBotSeats gives any seat still empty when play starts to the computer, so
// a half-full table can get going instead of stalling.
func fillBotSeats(rec *store.GameRecord) {
	// Nobody at the table twice, including whoever is already seated.
	seated := map[string]bool{}
	for _, s := range rec.Seats {
		if s.Taken && s.Name != "" {
			seated[s.Name] = true
		}
	}
	for i := range rec.Seats {
		if rec.Seats[i].Taken {
			continue
		}
		host, ok := freeHost(seated)
		if !ok {
			host = randomHost(cryptoPick)
		}
		seated[host.Name] = true
		rec.Seats[i].Taken = true
		rec.Seats[i].Bot = true
		rec.Seats[i].Token = randHex(16)
		rec.Seats[i].Skin = DefaultSkin
		rec.Seats[i].Name, rec.Seats[i].Country = host.Name, host.Country
	}
}

// RunBots takes computer turns until the context ends. Bots throw their
// opening die and play their moves after a short pause, so a game with one
// feels like a game with someone on the other side.
func (s *Server) RunBots(ctx context.Context) {
	rng := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	ticker := time.NewTicker(900 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.stepBots(ctx, rng)
		}
	}
}

func (s *Server) stepBots(ctx context.Context, rng *mrand.Rand) {
	games, err := s.st.ListActive(ctx, 40)
	if err != nil {
		return
	}
	for _, g := range games {
		if !g.HasBot() {
			continue
		}
		s.stepOneGame(ctx, g.ID, rng)
	}
}

func (s *Server) stepOneGame(ctx context.Context, id string, rng *mrand.Rand) {
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()

	rec, err := s.st.Get(ctx, id)
	if err != nil || rec.Status != "active" {
		return
	}

	// Opening throws.
	if rec.State.Phase == game.PhaseRoll {
		for _, seat := range rec.State.Pending {
			if !s.seatIsBot(rec, seat) {
				continue
			}
			if _, err := rec.State.RollDie(seat, rollDie); err != nil {
				continue
			}
			rec.Version++
			if err := s.st.Update(ctx, rec); err != nil {
				logf("bot roll save failed: %v", err)
			}
			return // one throw per tick, so the player sees them land one by one
		}
		return
	}

	if !s.seatIsBot(rec, rec.State.Turn) {
		return
	}
	seat := rec.State.Turn
	from, to, ok := s.board.ChooseMove(rec.State, seat, rng)
	if !ok {
		return // the engine will knock them out on the next turn change
	}
	moveRec, err := s.board.ApplyMove(rec.State, seat, from, to)
	if err != nil {
		logf("game %s: bot %s tried an illegal move %s->%s: %v", id, seat, from, to, err)
		return
	}
	if rec.State.Status == game.StatusFinished {
		rec.Status = "finished"
	}
	rec.Version++
	if err := s.st.Update(ctx, rec); err != nil {
		logf("bot move save failed: %v", err)
		return
	}
	if err := s.st.AppendMove(ctx, rec.ID, rec.State.Ply, moveRec); err != nil {
		logf("game %s: bot move history write failed: %v", rec.ID, err)
	}
	// The computer ends plenty of games — by winning, or by walling the last
	// rival in. Those count exactly as much as the ones a player ends, so the
	// tally has to happen here too and not only on the human move path.
	if rec.Status == "finished" {
		logf("game %s finished: winner=%s (%s)", rec.ID, rec.State.Winner, rec.State.WinReason)
		s.awardStats(ctx, rec)
	}
}

func (s *Server) seatIsBot(rec *store.GameRecord, seat game.Color) bool {
	for _, st := range rec.Seats {
		if st.Seat == seat {
			return st.Bot
		}
	}
	return false
}

// handleLobbyStart lets a host begin a table that is not full yet; the
// remaining seats are taken by the computer.
func (s *Server) handleLobbyStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	id := r.PathValue("id")
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()

	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown table")
		return
	}
	if _, ok := rec.SeatFor(req.Token); !ok {
		writeErr(w, http.StatusForbidden, "you are not at that table")
		return
	}
	if rec.Status != "waiting" {
		writeErr(w, http.StatusConflict, "that table has already started")
		return
	}
	fillBotSeats(rec)
	startGame(rec)
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not start the table")
		return
	}
	logf("game %s started early with %d computer seats", rec.ID, countBots(rec))
	writeJSON(w, http.StatusOK, toClientState(rec, req.Token))
}

func countBots(rec *store.GameRecord) int {
	n := 0
	for _, s := range rec.Seats {
		if s.Bot {
			n++
		}
	}
	return n
}
