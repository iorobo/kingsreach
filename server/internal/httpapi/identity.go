package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"kingsreach/internal/store"
)

// Player identity. A Steam sign-in is verified and keeps its progress; a guest
// just picks a name and keeps nothing beyond the session.

const (
	KindSteam = "steam"
	KindGuest = "guest"
)

// cleanName trims a display name to something safe to show to other players.
func cleanName(s string) string { return trimTo(s, 24) }

// cleanTableName allows a little more room than a nickname — a table name is a
// short sentence ("iemand zin in een potje?"), and 24 runes cut those in half.
// Matches the client's own maxlength.
func cleanTableName(s string) string { return trimTo(s, 40) }

func trimTo(s string, limit int) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1 // strip control characters
		}
		return r
	}, s)
	if len([]rune(s)) > limit {
		s = strings.TrimSpace(string([]rune(s)[:limit]))
	}
	return s
}

// cleanCountry keeps only a plain ISO-3166 alpha-2 code.
func cleanCountry(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 2 {
		return ""
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return ""
		}
	}
	return s
}

func hashPassword(password, salt string) string {
	if password == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(salt + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func newSalt() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type profilePayload struct {
	Token         string   `json:"token"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name"`
	Avatar        string   `json:"avatar"`
	Country       string   `json:"country"`
	Persistent    bool     `json:"persistent"` // does progress survive?
	GamesPlayed   int      `json:"gamesPlayed"`
	Wins          int      `json:"wins"`
	EquippedSkin  string   `json:"equippedSkin"`
	EquippedEnv   string   `json:"equippedEnv"`
	EquippedBoard string   `json:"equippedBoard"`
	Colour        string   `json:"colour"` // preferred stone colour, "" = no preference
	Unlocked      []string `json:"unlocked"`
	DevUnlockAll  bool     `json:"devUnlockAll"`
}

func toProfilePayload(p *store.Profile) *profilePayload {
	board := p.EquippedBoard
	if board == "" {
		board = DefaultBoard // profiles predating the board picker
	}
	return &profilePayload{
		Token: p.Token, Kind: p.Kind, Name: p.Name, Avatar: p.Avatar, Country: p.Country,
		Persistent: p.Persistent(), GamesPlayed: p.GamesPlayed, Wins: p.Wins,
		EquippedSkin: p.EquippedSkin, EquippedEnv: p.EquippedEnv, EquippedBoard: board,
		Colour:   p.Colour,
		Unlocked: unlockedIDs(p), DevUnlockAll: UnlockAll,
	}
}

// handleProfileCreate makes a guest identity from a chosen name.
func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Country string `json:"country"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	name := cleanName(req.Name)
	if name == "" {
		name = "Wanderer"
	}
	// The picker is normally already filled in from /api/geo; this covers the
	// case where that guess never arrived.
	country := cleanCountry(req.Country)
	if country == "" {
		country = s.guessCountry(r)
	}
	p := &store.Profile{
		ID: randHex(12), Token: randHex(16), Kind: KindGuest,
		Name: name, Country: country,
		EquippedSkin: DefaultSkin, EquippedEnv: DefaultEnv,
	}
	if err := s.st.CreateProfile(r.Context(), p); err != nil {
		logf("create guest profile failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not create profile")
		return
	}
	logf("guest %s created (%s)", p.ID, p.Name)
	writeJSON(w, http.StatusOK, toProfilePayload(p))
}

func (s *Server) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProfileByToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	s.fillMissingCountry(r, p)
	writeJSON(w, http.StatusOK, toProfilePayload(p))
}

// fillMissingCountry gives a flag to a profile that has none. Sign-in is not
// the only chance to get this right: a player may have signed in before the
// server could place addresses, or from behind a proxy that was not yet
// passing the real one through, and nothing else would ever fix that. Only
// ever fills a blank — a country the player chose is theirs.
func (s *Server) fillMissingCountry(r *http.Request, p *store.Profile) {
	if p == nil || p.Country != "" {
		return
	}
	c := s.guessCountry(r)
	if c == "" {
		return
	}
	p.Country = c
	if err := s.st.UpdateProfileIdentity(r.Context(), p); err != nil {
		logf("could not save the guessed country for %s: %v", p.ID, err)
		return
	}
	logf("profile %s had no country; placed it in %s", p.ID, c)
}

func orNone(s string) string {
	if s == "" {
		return "no country"
	}
	return s
}

// handleProfileUpdate lets a player change their display name or country.
// A Steam name comes from Steam and is not editable here.
func (s *Server) handleProfileUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string `json:"token"`
		Name    string `json:"name"`
		Country string `json:"country"`
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
	if p.Kind != KindSteam {
		if name := cleanName(req.Name); name != "" {
			p.Name = name
		}
	}
	if c := cleanCountry(req.Country); c != "" || req.Country == "" {
		p.Country = c
	}
	if err := s.st.UpdateProfileIdentity(r.Context(), p); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not save")
		return
	}
	writeJSON(w, http.StatusOK, toProfilePayload(p))
}

// ---- Steam sign-in ----

// handleSteamLogin sends the player off to Steam.
func (s *Server) handleSteamLogin(w http.ResponseWriter, r *http.Request) {
	if s.steam == nil {
		writeErr(w, http.StatusServiceUnavailable, "Steam sign-in is not configured")
		return
	}
	http.Redirect(w, r, s.steam.LoginURL(s.steam.Realm+"/api/auth/steam/return"), http.StatusFound)
}

// handleSteamReturn verifies the callback with Steam, then hands the browser
// back to the game with a token in the URL fragment (a fragment never reaches
// a server log or a Referer header).
func (s *Server) handleSteamReturn(w http.ResponseWriter, r *http.Request) {
	if s.steam == nil {
		writeErr(w, http.StatusServiceUnavailable, "Steam sign-in is not configured")
		return
	}
	fail := func(reason string) {
		logf("steam sign-in rejected: %s", reason)
		http.Redirect(w, r, "/#steam=failed", http.StatusFound)
	}

	steamID, err := s.steam.SteamIDFromCallback(r.Context(), r.URL.Query())
	if err != nil {
		fail(err.Error())
		return
	}

	prof, err := s.st.GetProfileBySteamID(r.Context(), steamID)
	if err != nil {
		// Steam's public profile carries no country without an API key, so the
		// flag comes from the address instead. Editable afterwards.
		prof = &store.Profile{
			ID: randHex(12), Token: randHex(16), Kind: KindSteam, SteamID: steamID,
			Name: "Steam player", Country: s.guessCountry(r),
			EquippedSkin: DefaultSkin, EquippedEnv: DefaultEnv,
		}
		if name, avatar, serr := s.steam.Summary(r.Context(), steamID); serr == nil && name != "" {
			prof.Name, prof.Avatar = cleanName(name), avatar
		}
		if err := s.st.CreateProfile(r.Context(), prof); err != nil {
			fail("could not create profile: " + err.Error())
			return
		}
		logf("steam player %s signed in for the first time (%s)", steamID, prof.Name)
	} else {
		// Refresh the name and avatar; people rename themselves.
		changed := false
		if name, avatar, serr := s.steam.Summary(r.Context(), steamID); serr == nil && name != "" {
			prof.Name, prof.Avatar = cleanName(name), avatar
			changed = true
		}
		// A profile made before we could place addresses — or before the proxy
		// was passing the real one through — has no country at all, and nothing
		// would ever have given it one.
		if prof.Country == "" {
			if c := s.guessCountry(r); c != "" {
				prof.Country = c
				changed = true
			}
		}
		if changed {
			if err := s.st.UpdateProfileIdentity(r.Context(), prof); err != nil {
				logf("steam profile refresh failed: %v", err)
			}
		}
		logf("steam player %s signed in (%s, %s)", steamID, prof.Name, orNone(prof.Country))
	}
	http.Redirect(w, r, "/#steam="+prof.Token, http.StatusFound)
}
