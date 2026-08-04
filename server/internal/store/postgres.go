package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"kingsreach/internal/game"
)

type Postgres struct {
	pool *pgxpool.Pool
}

const schema = `
CREATE TABLE IF NOT EXISTS games (
  id         text PRIMARY KEY,
  code       text UNIQUE NOT NULL,
  mode       text NOT NULL,
  status     text NOT NULL,
  token_west text NOT NULL,
  token_east text NOT NULL DEFAULT '',
  state      jsonb NOT NULL,
  version    integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS moves (
  id bigserial PRIMARY KEY,
  game_id text NOT NULL REFERENCES games(id) ON DELETE CASCADE,
  ply integer NOT NULL,
  piece text NOT NULL,
  from_node text NOT NULL,
  to_node text NOT NULL,
  path jsonb NOT NULL,
  captured text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS moves_game_idx ON moves(game_id, ply);
CREATE TABLE IF NOT EXISTS profiles (
  id            text PRIMARY KEY,
  token         text UNIQUE NOT NULL,
  games_played  integer NOT NULL DEFAULT 0,
  wins          integer NOT NULL DEFAULT 0,
  equipped_skin text NOT NULL DEFAULT 'clay',
  equipped_env  text NOT NULL DEFAULT 'picnic',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE games ADD COLUMN IF NOT EXISTS profile_west text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS profile_east text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS skin_west text NOT NULL DEFAULT 'clay';
ALTER TABLE games ADD COLUMN IF NOT EXISTS skin_east text NOT NULL DEFAULT 'clay';
-- Seats replaced the fixed west/east columns when the game grew to four
-- players; the old columns stay so rows written before that still load.
ALTER TABLE games ADD COLUMN IF NOT EXISTS players integer NOT NULL DEFAULT 2;
ALTER TABLE games ADD COLUMN IF NOT EXISTS seats jsonb NOT NULL DEFAULT '[]'::jsonb;
-- The legacy seat columns are no longer written, so they need defaults or
-- every insert trips their NOT NULL.
ALTER TABLE games ALTER COLUMN token_west SET DEFAULT '';
ALTER TABLE games ALTER COLUMN token_east SET DEFAULT '';
-- Lobby browser.
ALTER TABLE games ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS password_hash text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS password_salt text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS host_name text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS host_country text NOT NULL DEFAULT '';
ALTER TABLE games ADD COLUMN IF NOT EXISTS house boolean NOT NULL DEFAULT false;
ALTER TABLE games ADD COLUMN IF NOT EXISTS listed boolean NOT NULL DEFAULT true;
CREATE INDEX IF NOT EXISTS games_open_idx ON games(status, created_at DESC);
-- Player identity.
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'guest';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS avatar text NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS country text NOT NULL DEFAULT '';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS steam_id text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS profiles_steam_idx ON profiles(steam_id) WHERE steam_id <> '';
`

func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The database container may still be starting; retry briefly.
	var pingErr error
	for i := 0; i < 30; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		pingErr = pool.Ping(pingCtx)
		cancel()
		if pingErr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres unreachable: %w", pingErr)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Name() string                   { return "postgres" }
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *Postgres) Close()                         { p.pool.Close() }

func (p *Postgres) Create(ctx context.Context, rec *GameRecord) error {
	stateJSON, err := json.Marshal(rec.State)
	if err != nil {
		return err
	}
	seatsJSON, err := json.Marshal(rec.Seats)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	rec.CreatedAt, rec.UpdatedAt = now, now
	_, err = p.pool.Exec(ctx, `
		INSERT INTO games (id, code, mode, status, players, seats, state, version,
		                   name, password_hash, password_salt, host_name, host_country, house, listed,
		                   created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		rec.ID, strings.ToUpper(rec.Code), rec.Mode, rec.Status,
		rec.Players, seatsJSON, stateJSON, rec.Version,
		rec.Name, rec.PasswordHash, rec.PasswordSalt, rec.HostName, rec.HostCountry, rec.House, rec.Listed,
		rec.CreatedAt, rec.UpdatedAt)
	return err
}

func (p *Postgres) ListOpen(ctx context.Context, limit int) ([]*GameRecord, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+selectCols+`
		FROM games WHERE status = 'waiting' AND listed AND mode = 'online'
		ORDER BY house ASC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*GameRecord{}
	for rows.Next() {
		rec, err := p.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (p *Postgres) ListActive(ctx context.Context, limit int) ([]*GameRecord, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+selectCols+`
		FROM games WHERE status = 'active' ORDER BY updated_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*GameRecord{}
	for rows.Next() {
		rec, err := p.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (p *Postgres) Sweep(ctx context.Context, waitingOlderThan, activeOlderThan time.Duration) (int, error) {
	now := time.Now().UTC()
	tag, err := p.pool.Exec(ctx, `
		DELETE FROM games
		WHERE (status = 'waiting' AND updated_at < $1)
		   OR (status = 'active'  AND updated_at < $2)`,
		now.Add(-waitingOlderThan), now.Add(-activeOlderThan))
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (p *Postgres) scanOne(row pgx.Row) (*GameRecord, error) {
	rec := &GameRecord{}
	var stateJSON, seatsJSON []byte
	var legacyTokenWest, legacyTokenEast, legacySkinWest, legacySkinEast string
	var legacyProfileWest, legacyProfileEast string
	err := row.Scan(&rec.ID, &rec.Code, &rec.Mode, &rec.Status, &rec.Players, &seatsJSON,
		&legacyTokenWest, &legacyTokenEast, &legacyProfileWest, &legacyProfileEast,
		&legacySkinWest, &legacySkinEast,
		&rec.Name, &rec.PasswordHash, &rec.PasswordSalt, &rec.HostName, &rec.HostCountry,
		&rec.House, &rec.Listed,
		&stateJSON, &rec.Version, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.State = &game.State{}
	if err := json.Unmarshal(stateJSON, rec.State); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(seatsJSON, &rec.Seats); err != nil {
		return nil, err
	}
	if len(rec.Seats) == 0 {
		// Row predates the seats column: rebuild the two-player table from it.
		rec.Seats = []Seat{
			{Seat: game.West, Token: legacyTokenWest, Profile: legacyProfileWest,
				Skin: legacySkinWest, Taken: legacyTokenWest != ""},
			{Seat: game.East, Token: legacyTokenEast, Profile: legacyProfileEast,
				Skin: legacySkinEast, Taken: legacyTokenEast != ""},
		}
		rec.Players = 2
	}
	return rec, nil
}

const selectCols = `id, code, mode, status, players, seats,
	token_west, token_east, profile_west, profile_east, skin_west, skin_east,
	name, password_hash, password_salt, host_name, host_country, house, listed,
	state, version, created_at, updated_at`

func (p *Postgres) Get(ctx context.Context, id string) (*GameRecord, error) {
	return p.scanOne(p.pool.QueryRow(ctx, `SELECT `+selectCols+` FROM games WHERE id = $1`, id))
}

func (p *Postgres) GetByCode(ctx context.Context, code string) (*GameRecord, error) {
	return p.scanOne(p.pool.QueryRow(ctx, `SELECT `+selectCols+` FROM games WHERE code = $1`, strings.ToUpper(code)))
}

func (p *Postgres) Update(ctx context.Context, rec *GameRecord) error {
	stateJSON, err := json.Marshal(rec.State)
	if err != nil {
		return err
	}
	seatsJSON, err := json.Marshal(rec.Seats)
	if err != nil {
		return err
	}
	rec.UpdatedAt = time.Now().UTC()
	tag, err := p.pool.Exec(ctx, `
		UPDATE games SET status=$2, seats=$3, state=$4, version=$5, updated_at=$6,
		       name=$7, host_name=$8, host_country=$9 WHERE id=$1`,
		rec.ID, rec.Status, seatsJSON, stateJSON, rec.Version, rec.UpdatedAt,
		rec.Name, rec.HostName, rec.HostCountry)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const profileCols = `id, token, kind, name, avatar, country, steam_id,
	games_played, wins, equipped_skin, equipped_env, created_at, updated_at`

func scanProfile(row pgx.Row) (*Profile, error) {
	prof := &Profile{}
	err := row.Scan(&prof.ID, &prof.Token, &prof.Kind, &prof.Name, &prof.Avatar, &prof.Country,
		&prof.SteamID, &prof.GamesPlayed, &prof.Wins,
		&prof.EquippedSkin, &prof.EquippedEnv, &prof.CreatedAt, &prof.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return prof, nil
}

func (p *Postgres) CreateProfile(ctx context.Context, prof *Profile) error {
	now := time.Now().UTC()
	prof.CreatedAt, prof.UpdatedAt = now, now
	_, err := p.pool.Exec(ctx, `
		INSERT INTO profiles (id, token, kind, name, avatar, country, steam_id,
		                      games_played, wins, equipped_skin, equipped_env, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		prof.ID, prof.Token, prof.Kind, prof.Name, prof.Avatar, prof.Country, prof.SteamID,
		prof.GamesPlayed, prof.Wins, prof.EquippedSkin, prof.EquippedEnv, prof.CreatedAt, prof.UpdatedAt)
	return err
}

func (p *Postgres) GetProfileByToken(ctx context.Context, token string) (*Profile, error) {
	return scanProfile(p.pool.QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE token = $1`, token))
}

func (p *Postgres) GetProfileBySteamID(ctx context.Context, steamID string) (*Profile, error) {
	if steamID == "" {
		return nil, ErrNotFound
	}
	return scanProfile(p.pool.QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE steam_id = $1`, steamID))
}

func (p *Postgres) UpdateProfileIdentity(ctx context.Context, prof *Profile) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE profiles SET name=$2, avatar=$3, country=$4, updated_at=now() WHERE id=$1`,
		prof.ID, prof.Name, prof.Avatar, prof.Country)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) UpdateProfileEquip(ctx context.Context, id, skin, env string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE profiles SET equipped_skin=$2, equipped_env=$3, updated_at=now() WHERE id=$1`,
		id, skin, env)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// BumpProfileStats records a finished game. Guests keep no progress, so the
// filter lives here rather than in every caller.
func (p *Postgres) BumpProfileStats(ctx context.Context, id string, win bool) error {
	winInc := 0
	if win {
		winInc = 1
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE profiles SET games_played = games_played + 1, wins = wins + $2, updated_at=now()
		WHERE id=$1 AND kind = 'steam'`, id, winInc)
	return err
}

func (p *Postgres) AppendMove(ctx context.Context, gameID string, ply int, mv *game.MoveRecord) error {
	pathJSON, err := json.Marshal(mv.Path)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO moves (game_id, ply, piece, from_node, to_node, path, captured)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		gameID, ply, string(mv.Piece), string(mv.From), string(mv.To), pathJSON, string(mv.Captured))
	return err
}
