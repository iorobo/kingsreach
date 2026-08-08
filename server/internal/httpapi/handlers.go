package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(s.boardJSON)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "store": s.st.Name(), "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "store": s.st.Name()})
}

// handleConfig tells the client which sign-in routes exist, so it can offer
// Steam only where a realm is actually configured.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"steam":     s.steam != nil,
		"unlockAll": UnlockAll,
	})
}

// Game modes. "practice" was the old name for a table one person runs; it is
// still accepted so saved games from before the rename keep working.
const (
	ModeOnline  = "online"
	ModeOffline = "offline"
)

func normaliseMode(mode string) string {
	switch mode {
	case "", ModeOnline:
		return ModeOnline
	case ModeOffline, "practice":
		return ModeOffline
	}
	return "" // unknown
}

// rollDie returns a fair 1..6 from the same source as the table identifiers.
func rollDie() int {
	n, err := rand.Int(rand.Reader, big.NewInt(6))
	if err != nil {
		panic(err)
	}
	return int(n.Int64()) + 1
}

// startGame is called once every seat is filled. The game opens in its rolling
// phase: the players throw for the opening move themselves.
func startGame(rec *store.GameRecord) {
	rec.Status = "active"
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode     string `json:"mode"`
		Players  int    `json:"players"`
		Profile  string `json:"profile"`
		Name     string `json:"name"`     // table name for the lobby browser
		Password string `json:"password"` // empty = open to anyone
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional; defaults below
	mode := normaliseMode(req.Mode)
	if mode == "" {
		writeErr(w, http.StatusBadRequest, "mode must be online or offline")
		return
	}
	players := req.Players
	if players == 0 {
		players = 2
	}
	if players < 2 || players > 4 {
		writeErr(w, http.StatusBadRequest, "a table seats two to four players")
		return
	}

	token := randHex(16)
	skin, profileID := DefaultSkin, ""
	hostName, hostCountry, avatar := "Wanderer", "", ""
	if req.Profile != "" {
		if p, err := s.st.GetProfileByToken(r.Context(), req.Profile); err == nil {
			skin, profileID = p.EquippedSkin, p.ID
			hostCountry, avatar = p.Country, p.Avatar
			if p.Name != "" {
				hostName = p.Name
			}
		}
	}

	tableName := cleanTableName(req.Name)
	if tableName == "" {
		tableName = hostName + "'s table"
	}
	salt := newSalt()

	seatOrder := game.SeatOrder(players)
	rec := &store.GameRecord{
		ID:      randHex(12),
		Mode:    mode,
		Status:  "waiting",
		Players: players,
		State:   game.NewState(seatOrder, seatOrder[0]),
		Version: 1,

		Name:         tableName,
		PasswordSalt: salt,
		PasswordHash: hashPassword(req.Password, salt),
		HostName:     hostName,
		HostCountry:  hostCountry,
		Listed:       mode == ModeOnline,
	}
	for i, seat := range seatOrder {
		// The creator always takes the first seat; offline, one player runs
		// the whole table so a group can share the screen.
		mine := i == 0 || mode == ModeOffline
		st := store.Seat{Seat: seat, Skin: skin, Taken: mine}
		if mine {
			st.Token, st.Profile = token, profileID
			st.Name, st.Avatar, st.Country = hostName, avatar, hostCountry
			st.Rank = s.rankOf(r.Context(), profileID)
		}
		rec.Seats = append(rec.Seats, st)
	}
	if rec.FreeSeats() == 0 {
		startGame(rec)
	}

	// Join codes are only five characters; retry on the (rare) collision.
	var err error
	for i := 0; i < 6; i++ {
		rec.Code = randCode()
		if err = s.st.Create(r.Context(), rec); err == nil {
			break
		}
	}
	if err != nil {
		logf("create game failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not create game")
		return
	}
	logf("game %s created (mode=%s, players=%d, code=%s)", rec.ID, mode, players, rec.Code)
	writeJSON(w, http.StatusOK, stateWithToken{s.stateFor(rec, token), token})
}

// handleJoin takes a free seat at a table from the lobby browser.
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GameID   string `json:"gameId"`
		Password string `json:"password"`
		Profile  string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GameID == "" {
		writeErr(w, http.StatusBadRequest, "which table?")
		return
	}

	mu := s.lock(req.GameID)
	mu.Lock()
	defer mu.Unlock()
	rec, err := s.st.Get(r.Context(), req.GameID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "that table is gone")
		return
	}
	if rec.Status != "waiting" || rec.FreeSeats() == 0 {
		writeErr(w, http.StatusConflict, "that table is already full")
		return
	}
	if rec.Locked() && hashPassword(req.Password, rec.PasswordSalt) != rec.PasswordHash {
		writeErr(w, http.StatusForbidden, "wrong password")
		return
	}

	token := randHex(16)
	skin, profileID := DefaultSkin, ""
	name, country, avatar := "Wanderer", "", ""
	if req.Profile != "" {
		if p, perr := s.st.GetProfileByToken(r.Context(), req.Profile); perr == nil {
			skin, profileID = p.EquippedSkin, p.ID
			country, avatar = p.Country, p.Avatar
			if p.Name != "" {
				name = p.Name
			}
		}
	}

	// One identity, one seat. Two browser windows signed in to the same account
	// could otherwise sit down opposite each other, which is not a game — and
	// with a computer filling a third seat it hands out a guaranteed victory
	// every time. The seat you already hold is still yours; use the token the
	// server gave you then, rather than taking another.
	if profileID != "" {
		if seat, taken := rec.SeatForProfile(profileID); taken {
			logf("game %s: profile %s tried to take a second seat (already at %s)", rec.ID, profileID, seat.Seat)
			writeErr(w, http.StatusConflict, "you are already at this table")
			return
		}
	}

	for i := range rec.Seats {
		if !rec.Seats[i].Taken {
			rec.Seats[i].Taken = true
			rec.Seats[i].Token = token
			rec.Seats[i].Profile = profileID
			rec.Seats[i].Skin = skin
			rec.Seats[i].Name, rec.Seats[i].Avatar, rec.Seats[i].Country = name, avatar, country
			rec.Seats[i].Rank = s.rankOf(r.Context(), profileID)
			break
		}
	}
	if rec.FreeSeats() == 0 {
		startGame(rec)
		s.armClock(rec)
	}
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not join game")
		return
	}
	logf("game %s joined (%d seats free)", rec.ID, rec.FreeSeats())
	writeJSON(w, http.StatusOK, stateWithToken{s.stateFor(rec, token), token})
}

// loadForToken fetches the game and checks the caller holds a seat at it.
func (s *Server) loadForToken(w http.ResponseWriter, r *http.Request, token string) (*store.GameRecord, bool) {
	id := r.PathValue("id")
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return nil, false
	}
	if _, ok := rec.SeatFor(token); !ok {
		writeErr(w, http.StatusForbidden, "invalid token for this game")
		return nil, false
	}
	return rec, true
}

// seatToMove resolves which seat a token is allowed to act as right now. One
// token may hold several seats (practice), in which case it plays whichever is
// on the move.
func seatToMove(rec *store.GameRecord, token string) (game.Color, error) {
	if rec.HoldsEverySeat(token) {
		return rec.State.Turn, nil
	}
	seat, ok := rec.SeatFor(token)
	if !ok {
		return "", errors.New("no seat")
	}
	return seat.Seat, nil
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	rec, ok := s.loadForToken(w, r, token)
	if !ok {
		return
	}
	if v := r.URL.Query().Get("v"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n == rec.Version {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.stateFor(rec, token))
}

func (s *Server) handleMoves(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	rec, ok := s.loadForToken(w, r, token)
	if !ok {
		return
	}
	from := game.NodeID(r.URL.Query().Get("from"))
	type moveOption struct {
		To   string   `json:"to"`
		Path []string `json:"path"`
		// Captures names the piece standing there, so the client can light a
		// strike differently from an empty field — and knows that clicking
		// that stone means "take it" rather than "select it".
		Captures string `json:"captures,omitempty"`
	}
	out := struct {
		Moves []moveOption `json:"moves"`
	}{Moves: []moveOption{}}
	if rec.Status == "active" {
		occupied := map[game.NodeID]*game.Piece{}
		for _, p := range rec.State.Pieces {
			if !p.Captured {
				occupied[p.Node] = p
			}
		}
		for to, path := range s.board.LegalMovesFrom(rec.State, from) {
			opt := moveOption{To: string(to)}
			for _, n := range path {
				opt.Path = append(opt.Path, string(n))
			}
			if victim, ok := occupied[to]; ok {
				opt.Captures = string(victim.ID)
			}
			out.Moves = append(out.Moves, opt)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type moveReq struct {
	Token string `json:"token"`
	From  string `json:"from"`
	To    string `json:"to"`
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	var req moveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if _, ok := s.loadForToken(w, r, req.Token); !ok {
		return
	}

	id := r.PathValue("id")
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	if rec.Status == "waiting" {
		writeErr(w, http.StatusConflict, "waiting for the other players")
		return
	}
	mover, err := seatToMove(rec, req.Token)
	if err != nil {
		writeErr(w, http.StatusForbidden, "invalid token for this game")
		return
	}

	moveRec, err := s.board.ApplyMove(rec.State, mover, game.NodeID(req.From), game.NodeID(req.To))
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, game.ErrBadNode) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	s.armClock(rec)
	if rec.State.Status == game.StatusFinished {
		rec.Status = "finished"
	}
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save move")
		return
	}
	if err := s.st.AppendMove(r.Context(), rec.ID, rec.State.Ply, moveRec); err != nil {
		logf("game %s: move history write failed: %v", rec.ID, err)
	}
	if rec.Status == "finished" {
		logf("game %s finished: winner=%s (%s)", rec.ID, rec.State.Winner, rec.State.WinReason)
		s.awardStats(r.Context(), rec)
	}
	writeJSON(w, http.StatusOK, s.stateFor(rec, req.Token))
}

// handleRoll throws one die for the caller's seat. The value is generated here
// — the player's click is what makes it happen, so the animation shows a real
// roll rather than replaying one the server made earlier.
func (s *Server) handleRoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Seat  string `json:"seat"` // optional: which seat, when you hold several
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if _, ok := s.loadForToken(w, r, req.Token); !ok {
		return
	}

	id := r.PathValue("id")
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	if rec.Status != "active" {
		writeErr(w, http.StatusConflict, "the table is not ready")
		return
	}

	seat, err := rollingSeat(rec, req.Token, game.Color(req.Seat))
	if err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	value, err := rec.State.RollDie(seat, rollDie)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.armClock(rec)
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save the throw")
		return
	}
	logf("game %s: %s threw a %d", rec.ID, seat, value)
	writeJSON(w, http.StatusOK, stateWithRoll{s.stateFor(rec, req.Token), string(seat), value})
}

// rollingSeat picks which seat a token throws for. Holding the whole table
// (practice) means throwing for whoever is still owed a die.
func rollingSeat(rec *store.GameRecord, token string, want game.Color) (game.Color, error) {
	if rec.HoldsEverySeat(token) {
		if want != "" && rec.State.MayRoll(want) {
			return want, nil
		}
		if len(rec.State.Pending) > 0 {
			return rec.State.Pending[0], nil
		}
		return "", game.ErrNotRolling
	}
	seat, ok := rec.SeatFor(token)
	if !ok {
		return "", errors.New("no seat")
	}
	if !rec.State.MayRoll(seat.Seat) {
		return "", game.ErrNotYourRoll
	}
	return seat.Seat, nil
}

func (s *Server) handleResign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	if _, ok := s.loadForToken(w, r, req.Token); !ok {
		return
	}

	id := r.PathValue("id")
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	quitter, err := seatToMove(rec, req.Token)
	if err != nil {
		writeErr(w, http.StatusForbidden, "invalid token for this game")
		return
	}
	if err := rec.State.Resign(s.board, quitter); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if rec.State.Status == game.StatusFinished {
		rec.Status = "finished"
	}
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save")
		return
	}
	logf("game %s: %s resigned", rec.ID, quitter)
	if rec.Status == "finished" {
		s.awardStats(r.Context(), rec)
	}
	writeJSON(w, http.StatusOK, s.stateFor(rec, req.Token))
}
