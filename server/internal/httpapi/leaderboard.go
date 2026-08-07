package httpapi

import (
	"context"
	"net/http"
)

// The hall of champions.
//
// Twenty wins is the door. Below that you do not appear at all — not as a low
// rank, not greyed out, simply absent, which is what makes arriving on it feel
// like something. Guests can never appear: they keep no progress to rank.

// LeaderboardMinWins is how many victories buy a place on the board.
const LeaderboardMinWins = 20

const leaderboardLimit = 100

// rankOf is a player's place in the hall of champions, or 0 for everyone else.
// Snapshotted onto a seat when they sit down rather than resolved per poll —
// a leaderboard query on every state build would be absurd, and a rank that
// shifts mid-game changes nothing about the game.
func (s *Server) rankOf(ctx context.Context, profileID string) int {
	if profileID == "" {
		return 0
	}
	top, err := s.st.Leaderboard(ctx, LeaderboardMinWins, crownedPlaces)
	if err != nil {
		return 0
	}
	for i, p := range top {
		if p.ID == profileID {
			return i + 1
		}
	}
	return 0
}

// crownedPlaces is how far down the board a crown is worth wearing.
const crownedPlaces = 3

type rankEntry struct {
	// Absent rather than zero for someone not on the board — a rank of 0 is
	// not a position, and the client should not have to know that.
	Rank    int    `json:"rank,omitempty"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar,omitempty"`
	Country string `json:"country,omitempty"`
	Wins    int    `json:"wins"`
	Played  int    `json:"played"`
	You     bool   `json:"you,omitempty"`
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	// Knowing where you stand is the other half of a leaderboard, so the
	// caller's own record comes back even when they are nowhere near it.
	var me *rankEntry
	var needed int
	if token := r.URL.Query().Get("token"); token != "" {
		if p, err := s.st.GetProfileByToken(r.Context(), token); err == nil {
			me = &rankEntry{Name: p.Name, Avatar: p.Avatar, Country: p.Country,
				Wins: p.Wins, Played: p.GamesPlayed, You: true}
			if n := LeaderboardMinWins - p.Wins; n > 0 && p.Persistent() {
				needed = n
			}
		}
	}

	profiles, err := s.st.Leaderboard(r.Context(), LeaderboardMinWins, leaderboardLimit)
	if err != nil {
		logf("leaderboard failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read the standings")
		return
	}

	out := struct {
		MinWins int         `json:"minWins"`
		Entries []rankEntry `json:"entries"`
		You     *rankEntry  `json:"you,omitempty"`
		// WinsNeeded is how many more victories the caller needs to appear.
		// Zero once they are on the board, or when they cannot be ranked.
		WinsNeeded int `json:"winsNeeded"`
	}{MinWins: LeaderboardMinWins, Entries: []rankEntry{}, You: me, WinsNeeded: needed}

	for i, p := range profiles {
		entry := rankEntry{
			Rank: i + 1, Name: p.Name, Avatar: p.Avatar, Country: p.Country,
			Wins: p.Wins, Played: p.GamesPlayed,
		}
		if me != nil && p.Token == r.URL.Query().Get("token") {
			entry.You = true
			me.Rank = entry.Rank
		}
		out.Entries = append(out.Entries, entry)
	}
	writeJSON(w, http.StatusOK, out)
}
