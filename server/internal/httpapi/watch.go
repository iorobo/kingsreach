package httpapi

import (
	"net/http"
	"strconv"
)

// Watching a game you are not in.
//
// The board is the same board; the only difference is that a watcher holds no
// seat, so `you` comes back empty and every move endpoint keeps refusing them.
// That falls out of the existing token check rather than needing a second,
// weaker path through the rules — a spectator is simply somebody with no seat.
//
// One consequence worth stating rather than discovering: a game in progress
// can be watched by anyone who can see it in the board room, including while
// you are playing it. For the computer's exhibition matches that is the whole
// point. Between people it means an opponent's friend could watch over your
// shoulder from across the internet; if that ever matters, this is the place
// to gate it.

func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.st.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown game")
		return
	}
	// Only games the board room advertises. A shared-screen game belongs to
	// the people round that screen.
	if rec.Mode != ModeOnline {
		writeErr(w, http.StatusForbidden, "that game is not being shown")
		return
	}

	// Version-aware, exactly like the seated poll: nothing changed, nothing sent.
	if v := r.URL.Query().Get("v"); v != "" {
		if at, err := strconv.Atoi(v); err == nil && at == rec.Version {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	// An empty token yields a state with no seat, which is what a watcher is.
	writeJSON(w, http.StatusOK, toClientState(rec, ""))
}
