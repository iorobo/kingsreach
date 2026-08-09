package store

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"kingsreach/internal/game"
)

// Memory is a non-persistent Store for tests and DATABASE_URL-less demos.
type Memory struct {
	mu          sync.RWMutex
	byID        map[string]*GameRecord
	byCode      map[string]string
	profByID    map[string]*Profile
	profByToken map[string]string
	invites     map[string]*Invite
}

func NewMemory() *Memory {
	return &Memory{
		byID: map[string]*GameRecord{}, byCode: map[string]string{},
		profByID: map[string]*Profile{}, profByToken: map[string]string{},
		invites: map[string]*Invite{},
	}
}

func (m *Memory) Name() string                   { return "memory" }
func (m *Memory) Ping(ctx context.Context) error { return nil }
func (m *Memory) Close()                         {}

// cloneRecord hands out an independent copy: callers mutate seats and state in
// place, and the stored record must not change until they call Update.
func cloneRecord(rec *GameRecord) *GameRecord {
	cp := *rec
	if rec.State != nil {
		raw, _ := json.Marshal(rec.State)
		st := &game.State{}
		_ = json.Unmarshal(raw, st)
		cp.State = st
	}
	cp.Seats = append([]Seat(nil), rec.Seats...)
	return &cp
}

func (m *Memory) Create(ctx context.Context, rec *GameRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	rec.CreatedAt, rec.UpdatedAt = now, now
	m.byID[rec.ID] = cloneRecord(rec)
	m.byCode[strings.ToUpper(rec.Code)] = rec.ID
	return nil
}

func (m *Memory) Get(ctx context.Context, id string) (*GameRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRecord(rec), nil
}

func (m *Memory) GetByCode(ctx context.Context, code string) (*GameRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byCode[strings.ToUpper(code)]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRecord(m.byID[id]), nil
}

func (m *Memory) Update(ctx context.Context, rec *GameRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[rec.ID]; !ok {
		return ErrNotFound
	}
	rec.UpdatedAt = time.Now().UTC()
	m.byID[rec.ID] = cloneRecord(rec)
	return nil
}

func (m *Memory) AppendMove(ctx context.Context, gameID string, ply int, mv *game.MoveRecord) error {
	return nil // move history is a Postgres nicety; nothing to do in memory
}

func (m *Memory) list(match func(*GameRecord) bool, limit int) []*GameRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*GameRecord{}
	for _, rec := range m.byID {
		if match(rec) {
			out = append(out, cloneRecord(rec))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *Memory) ListOpen(ctx context.Context, limit int) ([]*GameRecord, error) {
	return m.list(func(r *GameRecord) bool {
		return r.Status == "waiting" && r.Listed && r.Mode == "online"
	}, limit), nil
}

func (m *Memory) ListActive(ctx context.Context, limit int) ([]*GameRecord, error) {
	return m.list(func(r *GameRecord) bool { return r.Status == "active" }, limit), nil
}

func (m *Memory) Sweep(ctx context.Context, waitingOlderThan, activeOlderThan time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	waitingCutoff := now.Add(-waitingOlderThan)
	activeCutoff := now.Add(-activeOlderThan)
	n := 0
	for id, rec := range m.byID {
		// Finished games go on the same cutoff as abandoned ones: the result
		// screen and the rematch window are long over, and the computer's own
		// matches would otherwise pile up a few hundred rows an hour.
		stale := (rec.Status == "waiting" && rec.UpdatedAt.Before(waitingCutoff)) ||
			(rec.Status != "waiting" && rec.UpdatedAt.Before(activeCutoff))
		if stale {
			delete(m.byID, id)
			delete(m.byCode, strings.ToUpper(rec.Code))
			n++
		}
	}
	return n, nil
}

func (m *Memory) GetProfileBySteamID(ctx context.Context, steamID string) (*Profile, error) {
	if steamID == "" {
		return nil, ErrNotFound
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.profByID {
		if p.SteamID == steamID {
			cp := *p
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) UpdateProfileIdentity(ctx context.Context, prof *Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profByID[prof.ID]
	if !ok {
		return ErrNotFound
	}
	p.Name, p.Avatar, p.Country = prof.Name, prof.Avatar, prof.Country
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) CreateProfile(ctx context.Context, p *Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt = now, now
	cp := *p
	m.profByID[p.ID] = &cp
	m.profByToken[p.Token] = p.ID
	return nil
}

func (m *Memory) GetProfileByToken(ctx context.Context, token string) (*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.profByToken[token]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *m.profByID[id]
	return &cp, nil
}

func (m *Memory) UpdateProfileEquip(ctx context.Context, id, skin, env, board, colour string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profByID[id]
	if !ok {
		return ErrNotFound
	}
	p.EquippedSkin, p.EquippedEnv, p.EquippedBoard, p.Colour = skin, env, board, colour
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) ProfilesBySteamIDs(ctx context.Context, steamIDs []string) ([]*Profile, error) {
	if len(steamIDs) == 0 {
		return nil, nil
	}
	want := make(map[string]bool, len(steamIDs))
	for _, id := range steamIDs {
		want[id] = true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Profile{}
	for _, p := range m.profByID {
		if p.SteamID != "" && want[p.SteamID] {
			q := *p
			out = append(out, &q)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Wins != out[j].Wins {
			return out[i].Wins > out[j].Wins
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (m *Memory) ListForProfile(ctx context.Context, profileID string, limit int) ([]*GameRecord, error) {
	if profileID == "" {
		return nil, nil
	}
	out := m.list(func(r *GameRecord) bool {
		if r.Status == "finished" || r.Mode != "online" {
			return false
		}
		_, ok := r.SeatForProfile(profileID)
		return ok
	}, limit)
	// Most recently touched first: this is a "where was I?" list, and the game
	// somebody is waiting on ought to be at the top.
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *Memory) CreateInvite(ctx context.Context, inv *Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	inv.CreatedAt = time.Now().UTC()
	// One standing invitation per player per table, matching the unique index
	// the real store enforces — and, like the upsert there, keeping the
	// existing row's id. A client holding an id from an earlier list must
	// still be able to dismiss it after the host clicked invite again.
	for id, existing := range m.invites {
		if existing.GameID == inv.GameID && existing.ToID == inv.ToID {
			inv.ID = id
			break
		}
	}
	cp := *inv
	m.invites[inv.ID] = &cp
	return nil
}

func (m *Memory) InvitesFor(ctx context.Context, profileID string) ([]*Invite, error) {
	if profileID == "" {
		return nil, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Invite{}
	for _, inv := range m.invites {
		if inv.ToID != profileID {
			continue
		}
		// An invitation to a table that has filled up or gone away is not an
		// invitation any more.
		if rec, ok := m.byID[inv.GameID]; !ok || rec.Status != "waiting" {
			continue
		}
		cp := *inv
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) DeleteInvite(ctx context.Context, id, profileID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv, ok := m.invites[id]; ok && inv.ToID == profileID {
		delete(m.invites, id)
	}
	return nil
}

// BumpProfileStats records a finished game. Guests keep no progress.
func (m *Memory) BumpProfileStats(ctx context.Context, id string, win bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profByID[id]
	if !ok {
		return ErrNotFound
	}
	if !p.Persistent() {
		return nil
	}
	p.GamesPlayed++
	if win {
		p.Wins++
	}
	p.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) Leaderboard(ctx context.Context, minWins, limit int) ([]*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Profile{}
	for _, p := range m.profByID {
		if !p.Persistent() || p.Wins < minWins {
			continue
		}
		q := *p
		out = append(out, &q)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Wins != out[j].Wins {
			return out[i].Wins > out[j].Wins
		}
		// Fewer games for the same number of wins is the better record.
		if out[i].GamesPlayed != out[j].GamesPlayed {
			return out[i].GamesPlayed < out[j].GamesPlayed
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
