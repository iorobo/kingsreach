package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"kingsreach/internal/store"
)

// The unlockable catalog. Server-authoritative: clients render whatever this
// lists and may only equip items the profile's stats have unlocked. Stats
// only advance in online games (practice never awards wins, and self-play
// with one profile counts as a single played game).
type CatalogItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // "skin" | "env"
	Name      string `json:"name"`
	Desc      string `json:"desc"`
	NeedPlays int    `json:"needPlays"`
	NeedWins  int    `json:"needWins"`
}

const (
	DefaultSkin  = "clay"
	DefaultEnv   = "picnic"
	DefaultBoard = "slate"
)

var catalog = []CatalogItem{
	{ID: "clay", Kind: "skin", Name: "Clay Buttons", Desc: "The classic ceramic set from the picnic blanket."},
	{ID: "royal", Kind: "skin", Name: "Royal Gold", Desc: "Gold against silver, as the court intended.", NeedWins: 1},
	{ID: "crystal", Kind: "skin", Name: "Crystal Court", Desc: "Glowing gems for a mystic duel.", NeedWins: 3},
	{ID: "rune", Kind: "skin", Name: "Runestones", Desc: "Weathered stones humming with old magic.", NeedPlays: 5},
	{ID: "picnic", Kind: "env", Name: "Sunlit Meadow", Desc: "Open grass, old trees and a bright afternoon."},
	{ID: "fair", Kind: "env", Name: "Medieval Fair", Desc: "Tents, banners and dusk at the tourney grounds.", NeedPlays: 2},
	{ID: "dust2", Kind: "env", Name: "Bombsite B", Desc: "A duel on the crates, somewhere hot and dusty.", NeedWins: 2},
	{ID: "store", Kind: "env", Name: "Boardgame Store", Desc: "Shelves of well-loved boxes and warm lamplight.", NeedWins: 4},
	{ID: "cafe", Kind: "env", Name: "Streetside Café", Desc: "A quiet corner table and strong coffee.", NeedPlays: 8},
	// Boards are a preference, not a prize: no requirements on any of them.
	{ID: "slate", Kind: "board", Name: "Slate", Desc: "The dark cloth the game grew up on."},
	{ID: "walnut", Kind: "board", Name: "Walnut", Desc: "Warm wood, like a board that gets used."},
	{ID: "ivory", Kind: "board", Name: "Ivory", Desc: "Pale and bright, for tired eyes."},
	{ID: "forest", Kind: "board", Name: "Forest", Desc: "Deep green felt, a card-room table."},
	{ID: "ink", Kind: "board", Name: "Ink", Desc: "Near black, so the stones do the talking."},
}

// UnlockAll makes every profile see the whole catalog as unlocked. It is set
// from the KINGSREACH_UNLOCK_ALL env var so a dev/preview deployment can show
// off all skins and environments without grinding out the wins. Stats are
// still tracked normally; only the gate is lifted.
var UnlockAll bool

func itemByID(id string) *CatalogItem {
	for i := range catalog {
		if catalog[i].ID == id {
			return &catalog[i]
		}
	}
	return nil
}

func isUnlocked(p *store.Profile, it *CatalogItem) bool {
	if UnlockAll {
		return true
	}
	return p.GamesPlayed >= it.NeedPlays && p.Wins >= it.NeedWins
}

func unlockedIDs(p *store.Profile) []string {
	out := []string{}
	for i := range catalog {
		if isUnlocked(p, &catalog[i]) {
			out = append(out, catalog[i].ID)
		}
	}
	return out
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": catalog})
}

func (s *Server) handleProfileEquip(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Skin  string `json:"skin"`
		Env   string `json:"env"`
		Board string `json:"board"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	p, err := s.st.GetProfileByToken(r.Context(), req.Token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	skin, env, board := p.EquippedSkin, p.EquippedEnv, p.EquippedBoard
	if board == "" {
		board = DefaultBoard
	}
	if req.Board != "" {
		it := itemByID(req.Board)
		if it == nil || it.Kind != "board" {
			writeErr(w, http.StatusBadRequest, "no such board")
			return
		}
		if !isUnlocked(p, it) {
			writeErr(w, http.StatusConflict, "that board is still locked")
			return
		}
		board = it.ID
	}
	if req.Skin != "" {
		it := itemByID(req.Skin)
		if it == nil || it.Kind != "skin" {
			writeErr(w, http.StatusBadRequest, "no such skin")
			return
		}
		if !isUnlocked(p, it) {
			writeErr(w, http.StatusConflict, "that skin is still locked")
			return
		}
		skin = it.ID
	}
	if req.Env != "" {
		it := itemByID(req.Env)
		if it == nil || it.Kind != "env" {
			writeErr(w, http.StatusBadRequest, "no such environment")
			return
		}
		if !isUnlocked(p, it) {
			writeErr(w, http.StatusConflict, "that environment is still locked")
			return
		}
		env = it.ID
	}
	if err := s.st.UpdateProfileEquip(r.Context(), p.ID, skin, env, board); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save")
		return
	}
	p.EquippedSkin, p.EquippedEnv, p.EquippedBoard = skin, env, board
	writeJSON(w, http.StatusOK, toProfilePayload(p))
}

// awardStats updates played/win counters when a game just finished.
//
// Three rules, and the middle one used to be wrong: it required two human
// profiles at the table, so beating the computer never recorded a win.
//
//   - Only signed-in players keep progress (enforced again in the store,
//     which ignores bumps for guests).
//   - A win needs a genuine opponent — someone else's profile, or the
//     computer. Beating the computer counts; sitting down against yourself in
//     two tabs does not.
//   - Offline games count for nothing. One person passing a mouse around is
//     not a result, and the mode exists precisely so it does not have to be.
//
// Takes a context rather than a request because the computer finishes plenty
// of games itself, and that path has no request to hand.
func (s *Server) awardStats(ctx context.Context, rec *store.GameRecord) {
	if rec.Mode != ModeOnline {
		return
	}
	winnerProfile := ""
	seatsPer := map[string]int{}
	bots := 0
	for _, seat := range rec.Seats {
		if seat.Bot {
			bots++
			continue
		}
		if seat.Profile == "" {
			continue
		}
		seatsPer[seat.Profile]++
		if seat.Seat == rec.State.Winner {
			winnerProfile = seat.Profile
		}
	}
	// Someone actually had to be on the other side of the board.
	contested := len(seatsPer) > 1 || bots > 0
	// Joining is supposed to refuse a second seat for the same account, so this
	// should never fire — but a win handed to someone playing themselves is
	// exactly the result worth being paranoid about, and rows predating that
	// check still exist.
	if seatsPer[winnerProfile] > 1 {
		logf("game %s: profile %s held %d seats; no win awarded",
			rec.ID, winnerProfile, seatsPer[winnerProfile])
		contested = false
	}
	for pid := range seatsPer {
		win := contested && pid == winnerProfile
		if err := s.st.BumpProfileStats(ctx, pid, win); err != nil {
			logf("game %s: stats update for profile %s failed: %v", rec.ID, pid, err)
		}
	}
}
