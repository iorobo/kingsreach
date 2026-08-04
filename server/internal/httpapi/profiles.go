package httpapi

import (
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
	DefaultSkin = "clay"
	DefaultEnv  = "picnic"
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
	skin, env := p.EquippedSkin, p.EquippedEnv
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
	if err := s.st.UpdateProfileEquip(r.Context(), p.ID, skin, env); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save")
		return
	}
	p.EquippedSkin, p.EquippedEnv = skin, env
	writeJSON(w, http.StatusOK, toProfilePayload(p))
}

// awardStats updates played/win counters when a game just finished. Only
// signed-in players keep progress, and a win only counts in an online game
// with more than one identity at the table.
func (s *Server) awardStats(r *http.Request, rec *store.GameRecord) {
	winnerProfile := ""
	distinct := map[string]bool{}
	for _, seat := range rec.Seats {
		if seat.Profile == "" || seat.Bot {
			continue
		}
		distinct[seat.Profile] = true
		if seat.Seat == rec.State.Winner {
			winnerProfile = seat.Profile
		}
	}
	realMatch := rec.Mode == "online" && len(distinct) > 1
	for pid := range distinct {
		win := realMatch && pid == winnerProfile
		if err := s.st.BumpProfileStats(r.Context(), pid, win); err != nil {
			logf("game %s: stats update for profile %s failed: %v", rec.ID, pid, err)
		}
	}
}
