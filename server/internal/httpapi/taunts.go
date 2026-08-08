package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Taunts: a short line you can send the rest of the table.
//
// The catalogue lives here rather than in the client, because the client is
// not a place to enforce anything. A player who wants to spam the table will
// not be stopped by a disabled button, so the limit is applied where the
// taunts actually pass through, and the client's own cooldown is only there to
// keep the button honest.
//
// Two limits, and they do different jobs:
//
//   - a gap between taunts, so nobody can machine-gun them;
//   - a ceiling per game, so nobody can drip-feed one every few seconds for an
//     entire match, which the gap alone would happily allow.

// Taunt is one line, with the sound that says it.
type Taunt struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Sound string `json:"sound"`
}

// The lines as recorded. Text must match what the voice says: it is what a
// player with the sound off reads instead, so a mismatch would be two
// different taunts wearing one name.
var taunts = []Taunt{
	{"nice-move", "Nice move", "audio/taunts/nice-move.mp3"},
	{"that-was-clever", "Okay, that was clever", "audio/taunts/that-was-clever.mp3"},
	{"nice-game", "Nice game!", "audio/taunts/nice-game.mp3"},
	{"didnt-see-that-coming", "Didn't see that one coming, did you", "audio/taunts/didnt-see-that-coming.mp3"},
	{"king-on-the-way", "My king is already on the way", "audio/taunts/king-on-the-way.mp3"},
	{"throne-awaits-me", "The throne awaits me", "audio/taunts/throne-awaits-me.mp3"},
	{"see-your-next-move", "I already see your next move", "audio/taunts/see-your-next-move.mp3"},
	{"wont-take-long", "This won't take long", "audio/taunts/wont-take-long.mp3"},
	{"bow-now", "Bow now, save yourself the trouble later", "audio/taunts/bow-now.mp3"},
}

func tauntByID(id string) *Taunt {
	for i := range taunts {
		if taunts[i].ID == id {
			return &taunts[i]
		}
	}
	return nil
}

const (
	// tauntGap is the wait between one taunt and the next.
	tauntGap = 8 * time.Second
	// tauntsPerGame is the ceiling for one player in one game. Generous
	// enough that nobody hits it in good faith, low enough that it cannot
	// become the whole match.
	tauntsPerGame = 12
	// tauntShelfLife is how long a taunt stays on the state for others to
	// pick up. Long enough for a poll to catch it, short enough that it does
	// not reappear when somebody reloads much later.
	tauntShelfLife = 12 * time.Second
)

// sentTaunt is a taunt on its way to the rest of the table.
type sentTaunt struct {
	Seat string    `json:"seat"`
	ID   string    `json:"id"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
	// Nonce changes every time, so a client can tell a repeat of the same
	// line from the same taunt arriving in two polls.
	Nonce string `json:"nonce"`
}

// tauntLog tracks what each seat has sent, per game. Held in memory: a
// restart forgiving somebody's spending is not worth a database column.
type tauntLog struct {
	mu    sync.Mutex
	sent  map[string][]time.Time // gameID+seat -> when
	last  map[string]sentTaunt   // gameID -> most recent, for pollers
	clock func() time.Time
}

func newTauntLog(clock func() time.Time) *tauntLog {
	return &tauntLog{sent: map[string][]time.Time{}, last: map[string]sentTaunt{}, clock: clock}
}

// allow reports whether this seat may taunt right now, and why not if it may
// not. Recording the send is part of allowing it, so the two cannot drift.
func (t *tauntLog) allow(gameID, seat string) (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	key := gameID + "\x00" + seat
	history := t.sent[key]

	if n := len(history); n > 0 && now.Sub(history[n-1]) < tauntGap {
		return false, "give it a moment"
	}
	if len(history) >= tauntsPerGame {
		return false, "that is enough taunting for one game"
	}
	t.sent[key] = append(history, now)
	return true, ""
}

func (t *tauntLog) publish(gameID string, s sentTaunt) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last[gameID] = s
	// The map only ever grows otherwise; games end and are swept, and a
	// forgotten taunt costs nothing.
	if len(t.last) > 500 {
		t.last = map[string]sentTaunt{gameID: s}
	}
}

// current returns the taunt to show for a game, if one is recent enough.
func (t *tauntLog) current(gameID string) *sentTaunt {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.last[gameID]
	if !ok || t.clock().Sub(s.At) > tauntShelfLife {
		return nil
	}
	out := s
	return &out
}

func (s *Server) handleTauntList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"taunts":    taunts,
		"gapMs":     tauntGap.Milliseconds(),
		"perGame":   tauntsPerGame,
		"shelfLife": tauntShelfLife.Milliseconds(),
	})
}

func (s *Server) handleTaunt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	id := r.PathValue("id")
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	seat, ok := rec.SeatFor(req.Token)
	if !ok {
		writeErr(w, http.StatusForbidden, "you are not at that table")
		return
	}
	if rec.Status != "active" {
		writeErr(w, http.StatusConflict, "there is nobody to say that to")
		return
	}
	// Nobody to hear it: a shared screen is one person, and the computer does
	// not care what you think of its play.
	if rec.Mode != ModeOnline {
		writeErr(w, http.StatusConflict, "there is nobody to say that to")
		return
	}
	line := tauntByID(req.ID)
	if line == nil {
		writeErr(w, http.StatusBadRequest, "no such taunt")
		return
	}
	if ok, why := s.taunts.allow(id, string(seat.Seat)); !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": why})
		return
	}

	s.taunts.publish(id, sentTaunt{
		Seat: string(seat.Seat), ID: line.ID, Text: line.Text,
		At: s.now(), Nonce: randHex(6),
	})

	// Bump the version so the others' polls stop answering "nothing changed".
	// Something did change at the table; it simply is not part of the board.
	// The taunt itself stays in memory — it is over in a few seconds and does
	// not belong in the saved game.
	mu := s.lock(id)
	mu.Lock()
	defer mu.Unlock()
	if fresh, err := s.st.Get(r.Context(), id); err == nil {
		fresh.Version++
		if err := s.st.Update(r.Context(), fresh); err != nil {
			logf("game %s: could not bump the version for a taunt: %v", id, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": line.ID})
}
