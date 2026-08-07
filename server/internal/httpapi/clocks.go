package httpapi

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

// Two clocks, both configurable.
//
//   - How long the computer pauses before moving. A bot that answers instantly
//     reads as a script, not an opponent, so it waits — a little at easy, a
//     while at hard.
//   - How long a person has to act before they forfeit their seat. Without it
//     one player closing a laptop freezes the table for everyone else.
//
// Both come from the environment so a deployment can tune them without a
// rebuild, and both are documented in the README rather than only here.

// Range is a span of think time to draw from.
type Range struct{ Min, Max time.Duration }

// Pick returns a duration somewhere in the range.
func (r Range) Pick(rng *rand.Rand) time.Duration {
	if r.Max <= r.Min {
		return r.Min
	}
	spread := int64(r.Max - r.Min)
	if rng == nil {
		return r.Min + time.Duration(spread/2)
	}
	return r.Min + time.Duration(rng.Int63n(spread))
}

// Timings holds everything the server needs to know about waiting.
type Timings struct {
	Think map[string]Range
	// Move is how long a person gets to roll or move. Zero disables the clock.
	Move time.Duration
}

var defaultThink = map[string]Range{
	game.Easy:   {600 * time.Millisecond, 1400 * time.Millisecond},
	game.Medium: {900 * time.Millisecond, 2200 * time.Millisecond},
	game.Hard:   {1400 * time.Millisecond, 3500 * time.Millisecond},
}

// DefaultMoveSeconds is how long a player has to act, unless told otherwise.
const DefaultMoveSeconds = 150

// TimingsFromEnv reads the four knobs. A malformed value warns and falls back
// to the default: a bad think-time is not worth refusing to boot over.
func TimingsFromEnv() Timings {
	t := Timings{Think: map[string]Range{}, Move: DefaultMoveSeconds * time.Second}
	for _, level := range game.Difficulties {
		key := "KINGSREACH_THINK_" + strings.ToUpper(level)
		t.Think[level] = defaultThink[level]
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		r, err := parseRange(raw)
		if err != nil {
			logf("%s=%q is not a min-max range in milliseconds (%v); using the default", key, raw, err)
			continue
		}
		t.Think[level] = r
	}
	if raw := strings.TrimSpace(os.Getenv("KINGSREACH_MOVE_SECONDS")); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs < 0 {
			logf("KINGSREACH_MOVE_SECONDS=%q is not a whole number of seconds; using %d", raw, DefaultMoveSeconds)
		} else {
			t.Move = time.Duration(secs) * time.Second
		}
	}
	return t
}

// LogTimings reports what the clocks ended up at, so a mistyped variable is
// visible in the log rather than only in odd behaviour hours later.
func (s *Server) LogTimings() {
	for _, level := range game.Difficulties {
		r := s.timings.Think[level]
		logf("%s bots pause %v–%v before moving", level, r.Min, r.Max)
	}
	if s.timings.Move <= 0 {
		logf("move clock disabled: a player can take as long as they like")
		return
	}
	logf("players have %v to roll or move before forfeiting their seat", s.timings.Move)
}

// parseRange reads "min-max" in milliseconds. A single number means "exactly".
func parseRange(raw string) (Range, error) {
	lo, hi, split := strings.Cut(raw, "-")
	if !split {
		hi = lo
	}
	min, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return Range{}, err
	}
	max, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil {
		return Range{}, err
	}
	if min < 0 || max < min {
		return Range{}, fmt.Errorf("range %d-%d is the wrong way round", min, max)
	}
	return Range{time.Duration(min) * time.Millisecond, time.Duration(max) * time.Millisecond}, nil
}

// ThinkTime is how long a bot of this difficulty should pause. Captures get a
// longer pause: a person takes longer over a decision that costs somebody a
// piece, and the bot should look like it did too.
func (t Timings) ThinkTime(difficulty string, weighty bool, rng *rand.Rand) time.Duration {
	r, ok := t.Think[difficulty]
	if !ok {
		r = t.Think[game.Medium]
	}
	d := r.Pick(rng)
	if weighty {
		d += d / 2
	}
	return d
}

// armClock gives whoever is next a fresh allowance — but only where the clock
// actually applies. Arming it offline would put a countdown on screen that
// nothing enforces, which is worse than having no clock at all: it tells the
// player something untrue.
func (s *Server) armClock(rec *store.GameRecord) {
	if rec.Mode != ModeOnline {
		rec.State.TurnDeadline = time.Time{}
		return
	}
	rec.State.ArmClock(s.now(), s.timings.Move)
}

// thinking tracks when each bot game may next act, so a bot appears to
// consider its move. Held in memory on purpose: a restart losing a pause
// costs nothing, and it keeps the pacing out of the schema.
type thinking struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func newThinking() *thinking { return &thinking{until: map[string]time.Time{}} }

// ready reports whether the bot in this game may act yet. The first time it is
// asked about a game it starts the pause rather than allowing the move, so a
// bot never answers the instant a player finishes.
func (t *thinking) ready(gameID string, now time.Time, pause time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	at, seen := t.until[gameID]
	if !seen {
		t.until[gameID] = now.Add(pause)
		return false
	}
	if now.Before(at) {
		return false
	}
	delete(t.until, gameID)
	return true
}

// forget drops a game's pause, so the next turn starts thinking afresh.
func (t *thinking) forget(gameID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.until, gameID)
}
