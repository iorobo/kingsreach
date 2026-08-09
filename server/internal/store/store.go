// Package store persists games. Two implementations: Postgres (production)
// and an in-process memory store (unit tests / DATABASE_URL-less demo mode).
package store

import (
	"context"
	"errors"
	"time"

	"kingsreach/internal/game"
)

var ErrNotFound = errors.New("game not found")

// Seat is one place at the table. Token is the secret that lets a client move
// for it; Profile/Skin link the seat to the progression system (both optional).
type Seat struct {
	Seat    game.Color `json:"seat"`
	Token   string     `json:"token"`
	Profile string     `json:"profile,omitempty"`
	Skin    string     `json:"skin"`
	Taken   bool       `json:"taken"`
	Bot     bool       `json:"bot,omitempty"` // played by the computer
	Name    string     `json:"name,omitempty"`
	Avatar  string     `json:"avatar,omitempty"`
	Country string     `json:"country,omitempty"`
	// Difficulty is how hard this computer player tries; empty for people.
	Difficulty string `json:"difficulty,omitempty"`
	// Rank is the player's leaderboard place, snapshotted when they sat down.
	// Resolving it per poll would mean a leaderboard query on every state
	// build, and a rank that shifts mid-game changes nothing. 0 = unranked.
	Rank int `json:"rank,omitempty"`
	// Wants is the colour this player asked for; Colour is what they were
	// given. Both are presentation only — the seat still decides where you sit
	// and where the camera looks from. Empty Colour means "the seat's own",
	// which is every game played before colours could be chosen.
	Wants  string `json:"wants,omitempty"`
	Colour string `json:"colour,omitempty"`
	// Team is the side this seat plays for; 0 in a free-for-all. Mirrors
	// game.State.Teams, which is where the rules read it from — this copy is
	// for the lobby, which has seats but no state worth consulting.
	Team int `json:"team,omitempty"`
}

// HasBot reports whether any seat is computer-played.
func (g *GameRecord) HasBot() bool {
	for _, s := range g.Seats {
		if s.Bot {
			return true
		}
	}
	return false
}

type GameRecord struct {
	ID      string
	Code    string
	Mode    string // "online" | "offline" ("practice" in rows predating the rename)
	Status  string // "waiting" | "active" | "finished"
	Players int    // seats at this table (2–4)
	Seats   []Seat
	State   *game.State
	Version int

	// Lobby presentation.
	Name         string // what the host called the table
	PasswordHash string // empty = open to anyone
	PasswordSalt string
	HostName     string
	HostCountry  string // ISO-3166 alpha-2, "" when the host did not say
	House        bool   // a table the server keeps open, not a player's
	Listed       bool   // shows in the lobby browser
	RematchID    string // the table this one's players moved on to, once agreed

	// Team play, as chosen by the host. Kept on the record as well as in the
	// state so the lobby browser can label a table before there is a game.
	Teams        bool
	FriendlyFire bool
	Inherit      bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Invite is one player asking another to a specific table. Rows are short
// lived: a table is swept within the hour, and an invitation to a table that
// no longer exists is noise.
type Invite struct {
	ID        string
	GameID    string
	FromID    string // profile that sent it
	FromName  string
	ToID      string // profile it is for
	TableName string
	CreatedAt time.Time
}

// Locked reports whether joining needs a password.
func (g *GameRecord) Locked() bool { return g.PasswordHash != "" }

// SeatFor finds the seat a token may play. In practice mode one token holds
// every seat, so the caller decides which one is on the move.
func (g *GameRecord) SeatFor(token string) (*Seat, bool) {
	if token == "" {
		return nil, false
	}
	for i := range g.Seats {
		if g.Seats[i].Taken && g.Seats[i].Token == token {
			return &g.Seats[i], true
		}
	}
	return nil, false
}

// SeatForProfile finds the seat an identity already holds, if any. Used to
// keep one account from occupying two places at the same table.
func (g *GameRecord) SeatForProfile(profileID string) (*Seat, bool) {
	if profileID == "" {
		return nil, false
	}
	for i := range g.Seats {
		if g.Seats[i].Taken && g.Seats[i].Profile == profileID {
			return &g.Seats[i], true
		}
	}
	return nil, false
}

// HoldsEverySeat reports whether one token controls the whole table.
func (g *GameRecord) HoldsEverySeat(token string) bool {
	if token == "" {
		return false
	}
	for _, s := range g.Seats {
		if !s.Taken || s.Token != token {
			return false
		}
	}
	return len(g.Seats) > 0
}

func (g *GameRecord) FreeSeats() int {
	n := 0
	for _, s := range g.Seats {
		if !s.Taken {
			n++
		}
	}
	return n
}

func (g *GameRecord) SkinOf(seat game.Color) string {
	for _, s := range g.Seats {
		if s.Seat == seat {
			return s.Skin
		}
	}
	return ""
}

// SeatByColor finds a seat by its board position.
func (g *GameRecord) SeatByColor(c game.Color) *Seat {
	for i := range g.Seats {
		if g.Seats[i].Seat == c {
			return &g.Seats[i]
		}
	}
	return nil
}

// TeamMap is the seats' sides, in the shape the engine wants. Nil when the
// table is a free-for-all.
func (g *GameRecord) TeamMap() map[game.Color]int {
	if !g.Teams {
		return nil
	}
	out := map[game.Color]int{}
	for _, s := range g.Seats {
		if s.Team != 0 {
			out[s.Seat] = s.Team
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Profile is a player identity, held by a secret token the client stores.
// Kind is "steam" for a verified Steam sign-in — only those keep progress —
// or "guest" for someone who just typed a name.
type Profile struct {
	ID            string
	Token         string
	Kind          string // "steam" | "guest"
	Name          string
	Avatar        string // URL, from Steam
	Country       string // ISO-3166 alpha-2
	SteamID       string
	GamesPlayed   int
	Wins          int
	EquippedSkin  string
	EquippedEnv   string
	EquippedBoard string
	// Colour is the seat colour this player would rather have. Empty means no
	// preference, which is also what everyone had before the picker existed.
	Colour    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Persistent reports whether this identity keeps its progress between visits.
func (p *Profile) Persistent() bool { return p.Kind == "steam" }

type Store interface {
	Create(ctx context.Context, rec *GameRecord) error
	Get(ctx context.Context, id string) (*GameRecord, error)
	GetByCode(ctx context.Context, code string) (*GameRecord, error)
	Update(ctx context.Context, rec *GameRecord) error
	AppendMove(ctx context.Context, gameID string, ply int, mv *game.MoveRecord) error
	// ListOpen returns tables still waiting for players, newest first.
	ListOpen(ctx context.Context, limit int) ([]*GameRecord, error)
	// ListActive returns games in progress, so the computer can take its turns.
	ListActive(ctx context.Context, limit int) ([]*GameRecord, error)
	// ListForProfile returns the unfinished tables a profile holds a seat at,
	// most recently touched first. This is what lets one account play several
	// games at once and find its way back to each of them.
	ListForProfile(ctx context.Context, profileID string, limit int) ([]*GameRecord, error)
	// Sweep clears out the leftovers: waiting tables nobody joined, and games
	// abandoned mid-play. Without the second cutoff every walked-away game
	// stays "active" for ever and clogs the board room.
	Sweep(ctx context.Context, waitingOlderThan, activeOlderThan time.Duration) (int, error)

	CreateProfile(ctx context.Context, p *Profile) error
	GetProfileByToken(ctx context.Context, token string) (*Profile, error)
	GetProfileBySteamID(ctx context.Context, steamID string) (*Profile, error)
	UpdateProfileIdentity(ctx context.Context, p *Profile) error
	UpdateProfileEquip(ctx context.Context, id, skin, env, board, colour string) error
	BumpProfileStats(ctx context.Context, id string, win bool) error
	// Leaderboard returns signed-in players with at least minWins victories,
	// best first. Guests are never listed — they keep no progress to rank.
	Leaderboard(ctx context.Context, minWins, limit int) ([]*Profile, error)
	// ProfilesBySteamIDs looks up the profiles behind a set of SteamIDs. This
	// is the whole friend list: Steam says who your friends are, and this says
	// which of them have ever played Kingsreach.
	ProfilesBySteamIDs(ctx context.Context, steamIDs []string) ([]*Profile, error)

	CreateInvite(ctx context.Context, inv *Invite) error
	// InvitesFor returns the invitations waiting for a profile, newest first.
	InvitesFor(ctx context.Context, profileID string) ([]*Invite, error)
	// DeleteInvite removes one invitation, whether accepted or turned down.
	DeleteInvite(ctx context.Context, id, profileID string) error

	Name() string
	Ping(ctx context.Context) error
	Close()
}
