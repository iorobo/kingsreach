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

// runningEntry is a table already under way. It cannot be joined, but it can
// be watched — which is why it carries a real game id now. Every one of these
// is a genuine game: the server plays a handful of exhibition matches between
// its own computer players, so the room is populated by games that actually
// exist rather than by entries invented for the list.
type runningEntry struct {
	GameID  string `json:"gameId"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Country string `json:"country"`
	Players int    `json:"players"`
	Ply     int    `json:"ply"`
	Minutes int    `json:"minutes"`
	// Bots is how many seats the computer is playing, so the browser can say
	// "computer match" rather than implying people are at the table.
	Bots int `json:"bots"`
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

// runningTables lists the games under way. Every entry is a real game with a
// real id, because the server keeps a few exhibition matches between its own
// computer players going (see seedExhibitions). The list used to be padded
// with invented rows; those could not be watched, and now there is nothing to
// invent.
//
// seenHost and seenName carry the entries already shown in the open list, so
// the same person never turns up twice in the same room.
func (s *Server) runningTables(ctx context.Context, seenHost, seenName map[string]bool) []runningEntry {
	out := []runningEntry{}
	games, err := s.st.ListActive(ctx, 40)
	if err != nil {
		return out
	}
	for _, g := range games {
		// A shared-screen game is one person playing themselves, and a game
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
			GameID: g.ID, Name: g.Name, Host: host, Country: g.HostCountry,
			Players: g.Players, Ply: g.State.Ply,
			Minutes: int(time.Since(g.CreatedAt).Minutes()),
			Bots:    countBots(g),
		})
		if len(out) == 12 {
			break
		}
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
	s.seedExhibitions(ctx, taken)
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

// seedExhibitions keeps a few computer-versus-computer games running.
//
// They exist so the board room shows real games. The list used to be padded
// with invented rows, which looked the same but could not be opened, counted
// or watched — and the moment anyone wanted to watch one, the difference
// mattered. These are ordinary games: the same engine, the same bots, the
// same move history, just with nobody human at the table.
func (s *Server) seedExhibitions(ctx context.Context, taken map[string]bool) {
	active, err := s.st.ListActive(ctx, 40)
	if err != nil {
		return
	}
	live := 0
	for _, g := range active {
		if g.House && g.Status == "active" && allBots(g) {
			live++
		}
	}
	if s.showTarget == 0 || live == 0 {
		s.showTarget = randRange(2, 6)
	}
	for live < s.showTarget {
		if err := s.createExhibition(ctx, taken); err != nil {
			logf("could not start an exhibition game: %v", err)
			return
		}
		live++
	}
}

func allBots(g *store.GameRecord) bool {
	for _, s := range g.Seats {
		if !s.Bot {
			return false
		}
	}
	return len(g.Seats) > 0
}

func (s *Server) createExhibition(ctx context.Context, taken map[string]bool) error {
	players := 2
	if randRange(1, 3) == 1 {
		players = randRange(3, 4)
	}
	seatOrder := game.SeatOrder(players)
	host, ok := freeHost(taken)
	if !ok {
		return nil // everybody is busy; try again next pass
	}
	taken[host.Name] = true

	rec := &store.GameRecord{
		ID: randHex(12), Mode: ModeOnline, Status: "active", Players: players,
		State:    game.NewState(seatOrder, seatOrder[0]),
		Version:  1,
		Name:     tableNameFor(host, cryptoPick),
		HostName: host.Name, HostCountry: host.Country,
		House: true,
		// Not in the open list: it is already under way, and it is there to be
		// watched rather than joined.
		Listed: false,
	}
	for i, seat := range seatOrder {
		who := host
		if i > 0 {
			next, ok := freeHost(taken)
			if !ok {
				return nil
			}
			who = next
			taken[who.Name] = true
		}
		rec.Seats = append(rec.Seats, store.Seat{
			Seat: seat, Skin: pick(exhibitionSkins), Taken: true, Bot: true,
			Token: randHex(16), Name: who.Name, Country: who.Country,
			Difficulty: pick(game.Difficulties),
		})
	}
	var err error
	for i := 0; i < 6; i++ {
		rec.Code = randCode()
		if err = s.st.Create(ctx, rec); err == nil {
			logf("exhibition game %s started (%d computer players)", rec.ID, players)
			return nil
		}
	}
	return err
}

// Exhibition games show off the skins rather than all wearing the default.
var exhibitionSkins = []string{"clay", "royal", "crystal", "rune"}

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
			st.Difficulty = pick(game.Difficulties)
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
		rec.Seats[i].Difficulty = pick(game.Difficulties)
	}
}

// RunBots drives everything that has to happen without anybody asking: the
// computer taking its turns, and the clock running out on players who have
// walked away. Both need to work whether or not a browser is polling.
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
		// The move clock applies to every table, and a game with no bot in it
		// is exactly the human-versus-human case that needs it most — this
		// loop used to skip those entirely.
		s.sweepClock(ctx, g.ID)
		if g.HasBot() {
			s.stepOneGame(ctx, g.ID, rng)
		}
	}
}

// sweepClock knocks out anyone who has run out of time. Bots are exempt: they
// have their own bounded pause and cannot stall.
func (s *Server) sweepClock(ctx context.Context, id string) {
	if s.timings.Move <= 0 {
		return
	}
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()

	rec, err := s.st.Get(ctx, id)
	if err != nil || rec.Status != "active" || rec.Mode != ModeOnline {
		return
	}
	now := s.now()
	overdue := rec.State.Overdue(now)
	if len(overdue) == 0 {
		return
	}

	knocked := 0
	for _, seat := range overdue {
		if s.seatIsBot(rec, seat) {
			continue
		}
		if err := rec.State.TimeOut(s.board, seat); err != nil {
			continue
		}
		logf("game %s: %s ran out of time", rec.ID, seat)
		knocked++
	}
	// Nobody human was overdue — only the computer, which is about to act
	// anyway. Give the clock a fresh turn rather than leaving it expired and
	// sweeping the same game every tick.
	s.armClock(rec)
	if knocked == 0 {
		if err := s.st.Update(ctx, rec); err != nil {
			logf("clock re-arm failed for %s: %v", rec.ID, err)
		}
		return
	}

	if rec.State.Status == game.StatusFinished {
		rec.Status = "finished"
	}
	rec.Version++
	if err := s.st.Update(ctx, rec); err != nil {
		logf("timeout save failed for %s: %v", rec.ID, err)
		return
	}
	if rec.Status == "finished" {
		logf("game %s finished on the clock: winner=%s (%s)", rec.ID, rec.State.Winner, rec.State.WinReason)
		s.awardStats(ctx, rec)
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
			s.armClock(rec)
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
	difficulty := s.seatDifficulty(rec, seat)

	// Pause before answering. A reply that lands the instant you let go of
	// your own stone reads as a script rather than an opponent.
	weighty := rec.State.LastMove != nil && rec.State.LastMove.Captured != ""
	if !s.think.ready(id, s.now(), s.timings.ThinkTime(difficulty, weighty, rng)) {
		return
	}

	from, to, ok := s.board.ChooseMove(rec.State, seat, difficulty, rng)
	if !ok {
		return // the engine will knock them out on the next turn change
	}
	moveRec, err := s.board.ApplyMove(rec.State, seat, from, to)
	if err != nil {
		logf("game %s: bot %s tried an illegal move %s->%s: %v", id, seat, from, to, err)
		return
	}
	s.armClock(rec)
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

func (s *Server) seatDifficulty(rec *store.GameRecord, seat game.Color) string {
	for _, st := range rec.Seats {
		if st.Seat == seat && st.Difficulty != "" {
			return st.Difficulty
		}
	}
	return game.Medium
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
	s.armClock(rec)
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not start the table")
		return
	}
	logf("game %s started early with %d computer seats", rec.ID, countBots(rec))
	writeJSON(w, http.StatusOK, s.stateFor(rec, req.Token))
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
