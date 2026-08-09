package httpapi

import (
	"encoding/json"
	"net/http"

	"kingsreach/internal/game"
)

// Booting a player who has run out of time.
//
// The clock already knows how to knock somebody out — the ticker sweeps every
// table and enforces it. So why a button?
//
// Because the ticker runs every 900 ms against every active game, and the
// person staring at an expired clock has no way to tell the difference between
// "the server is about to handle this" and "the server has forgotten about
// us". A button that does nothing the sweeper would not have done a second
// later is still worth having: it converts waiting into an action. It is also
// the honest fallback if the ticker is ever wedged.
//
// The rule it enforces is exactly the sweeper's, which is what keeps it from
// being a grief tool: you can only boot somebody whose time is *already* up,
// and only from a seat of your own.

func (s *Server) handleBoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Seat  string `json:"seat"`
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
	caller, ok := rec.SeatFor(req.Token)
	if !ok {
		writeErr(w, http.StatusForbidden, "invalid token for this game")
		return
	}
	target := game.Color(req.Seat)
	if target == caller.Seat {
		writeErr(w, http.StatusBadRequest, "resign if you want to leave")
		return
	}

	// The clock is the only authority. If it has not run out, there is nothing
	// to enforce and the answer is no — otherwise this is a button for throwing
	// people out of games you are losing.
	overdue := false
	for _, c := range rec.State.Overdue(s.now()) {
		if c == target {
			overdue = true
			break
		}
	}
	if !overdue {
		writeErr(w, http.StatusConflict, "their time has not run out")
		return
	}
	if seat := rec.SeatByColor(target); seat != nil && seat.Bot {
		// A bot cannot be out of time — it thinks on a bounded budget — so this
		// should be unreachable. Refusing is cheaper than reasoning about it.
		writeErr(w, http.StatusConflict, "the computer does not run out of time")
		return
	}

	if err := rec.State.TimeOut(s.board, target); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.armClock(rec)
	if rec.State.Status == game.StatusFinished {
		rec.Status = "finished"
	}
	rec.Version++
	if err := s.st.Update(r.Context(), rec); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save")
		return
	}
	logf("game %s: %s booted %s for running out of time", rec.ID, caller.Seat, target)
	if rec.Status == "finished" {
		s.awardStats(r.Context(), rec)
	}
	writeJSON(w, http.StatusOK, s.stateFor(rec, req.Token))
}
