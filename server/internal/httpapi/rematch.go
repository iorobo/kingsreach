package httpapi

import (
	"encoding/json"
	"net/http"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

// Playing the same people again.
//
// The endpoint is idempotent, and that is the whole design: whoever asks first
// creates the new table and the finished game remembers it, so everybody else
// asking afterwards lands at the same table instead of starting their own. The
// old game keeps the pointer, which is also how a player who is still staring
// at the result screen finds out that somebody wants another go — their poll
// picks up `rematchId` without needing any push.
//
// Against the computer this is instant. Between people it is genuinely "both
// of us agreed", because nobody is moved anywhere until they ask.

func (s *Server) handleRematch(w http.ResponseWriter, r *http.Request) {
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

	old, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	mySeat, ok := old.SeatFor(req.Token)
	if !ok {
		writeErr(w, http.StatusForbidden, "you were not at that table")
		return
	}
	if old.Status != "finished" {
		writeErr(w, http.StatusConflict, "that game is still going")
		return
	}

	// Somebody already asked: join the table they made rather than a second one.
	if old.RematchID != "" {
		next, err := s.st.Get(r.Context(), old.RematchID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "that rematch is gone")
			return
		}
		seat, ok := next.SeatForProfile(mySeat.Profile)
		if !ok || mySeat.Profile == "" {
			// No profile to match on (or the seat was reshuffled): fall back to
			// the same board position we held last time.
			seat = seatAt(next, mySeat.Seat)
		}
		if seat == nil {
			writeErr(w, http.StatusConflict, "there is no seat for you at the rematch")
			return
		}
		writeJSON(w, http.StatusOK, stateWithToken{s.stateFor(next, seat.Token), seat.Token})
		return
	}

	next := rebuild(old)
	var createErr error
	for i := 0; i < 6; i++ {
		next.Code = randCode()
		if createErr = s.st.Create(r.Context(), next); createErr == nil {
			break
		}
	}
	if createErr != nil {
		logf("rematch of %s failed: %v", old.ID, createErr)
		writeErr(w, http.StatusInternalServerError, "could not set up a rematch")
		return
	}

	old.RematchID = next.ID
	old.Version++
	if err := s.st.Update(r.Context(), old); err != nil {
		// The new table exists and is playable; losing the pointer only means
		// the others cannot follow us to it, so say so rather than pretend.
		logf("rematch pointer save failed for %s: %v", old.ID, err)
	}
	logf("game %s: rematch opened as %s", old.ID, next.ID)

	seat := seatAt(next, mySeat.Seat)
	writeJSON(w, http.StatusOK, stateWithToken{s.stateFor(next, seat.Token), seat.Token})
}

// rebuild lays out the same table again: same people, same computer players,
// same seats, fresh everything else. New seat tokens, because the old ones
// belong to a game that is over.
func rebuild(old *store.GameRecord) *store.GameRecord {
	seatOrder := game.SeatOrder(old.Players)
	next := &store.GameRecord{
		ID: randHex(12), Mode: old.Mode, Status: "waiting", Players: old.Players,
		State: game.NewState(seatOrder, seatOrder[0]), Version: 1,
		Name: old.Name, HostName: old.HostName, HostCountry: old.HostCountry,
		House: old.House,
		// A rematch is a private arrangement between the people who just
		// played, not an advert for anyone else to join.
		Listed: false,
	}
	for _, want := range seatOrder {
		seat := store.Seat{Seat: want, Skin: DefaultSkin}
		if from := seatAt(old, want); from != nil {
			seat = *from
			seat.Seat = want
			seat.Token = randHex(16)
		}
		next.Seats = append(next.Seats, seat)
	}
	if next.FreeSeats() == 0 {
		startGame(next)
	}
	return next
}

func seatAt(rec *store.GameRecord, at game.Color) *store.Seat {
	for i := range rec.Seats {
		if rec.Seats[i].Seat == at {
			return &rec.Seats[i]
		}
	}
	return nil
}
