package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"kingsreach/internal/game"
)

// The move clock, tested by moving the clock rather than by waiting on it.

// atTime points the server at a fixed instant and returns a way to advance it.
func atTime(srv *Server) func(time.Duration) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
}

// twoPlayerGame sets up an online table with one human and one computer, ready
// to roll. Returns the game id and the human's seat token.
func twoPlayerGame(t *testing.T, ts *httptest.Server) (string, string) {
	t.Helper()
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "name": "clock check"})
	id, token := g["gameId"].(string), g["token"].(string)
	code, st := request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})
	if code != 200 {
		t.Fatalf("start: %d %v", code, st)
	}
	return id, token
}

func TestSittingOnYourHandsLosesTheGame(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	// Get past the opening throws: both seats roll until play begins.
	for i := 0; i < 40; i++ {
		rec, err := srv.st.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if rec.State.Phase == game.PhasePlay {
			break
		}
		request(t, "POST", ts.URL+"/api/games/"+id+"/roll", map[string]string{"token": token})
		srv.stepBots(context.Background(), nil)
	}

	rec, _ := srv.st.Get(context.Background(), id)
	if rec.State.Phase != game.PhasePlay {
		t.Skip("the opening throws did not settle in this run")
	}
	mySeat, _ := rec.SeatFor(token)
	if rec.State.Turn != mySeat.Seat {
		t.Skip("the computer opens in this run; nothing for the human clock to catch")
	}

	// Now do nothing at all, for longer than the clock allows.
	advance(srv.timings.Move + time.Second)
	srv.sweepClock(context.Background(), id)

	rec, err := srv.st.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "finished" {
		t.Fatalf("the game should be over, status = %s", rec.Status)
	}
	if rec.State.Winner == mySeat.Seat {
		t.Fatalf("the player who walked away won: %v", rec.State.Winner)
	}
	if !rec.State.IsOut(mySeat.Seat) {
		t.Fatalf("the player who ran out of time is still in the game")
	}
	for _, k := range rec.State.Out {
		if k.Seat == mySeat.Seat && k.Cause != game.OutTimedOut {
			t.Fatalf("knocked out for %q, want %q", k.Cause, game.OutTimedOut)
		}
	}
}

// Never rolling stalls a table just as effectively as never moving.
func TestNeverRollingAlsoRunsOut(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	rec, _ := srv.st.Get(context.Background(), id)
	if rec.State.Phase != game.PhaseRoll {
		t.Fatalf("a fresh table should be rolling, got %s", rec.State.Phase)
	}
	mySeat, _ := rec.SeatFor(token)

	advance(srv.timings.Move + time.Second)
	srv.sweepClock(context.Background(), id)

	rec, _ = srv.st.Get(context.Background(), id)
	if !rec.State.IsOut(mySeat.Seat) {
		t.Fatalf("a player who never throws should be knocked out")
	}
}

// The computer has its own bounded pause and must never be timed out.
func TestTheComputerIsNeverTimedOut(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	rec, _ := srv.st.Get(context.Background(), id)
	mySeat, _ := rec.SeatFor(token)

	// Take the human out of the equation: let them roll, then jump the clock.
	request(t, "POST", ts.URL+"/api/games/"+id+"/roll", map[string]string{"token": token})
	advance(srv.timings.Move * 10)
	srv.sweepClock(context.Background(), id)

	rec, _ = srv.st.Get(context.Background(), id)
	for _, seat := range rec.Seats {
		if seat.Bot && rec.State.IsOut(seat.Seat) {
			t.Fatalf("the computer was timed out at %s", seat.Seat)
		}
	}
	// And the sweep must have re-armed rather than leaving an expired clock.
	if rec.State.Status == game.StatusActive && !rec.State.TurnDeadline.After(srv.now()) {
		t.Fatalf("the clock was left expired; it would sweep this game every tick")
	}
	_ = mySeat
}

// Four seats: running out knocks one player out and the others play on.
func TestTimeoutAtAFullTableDoesNotEndIt(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 4, "name": "four up"})
	id, token := g["gameId"].(string), g["token"].(string)
	request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})

	rec, _ := srv.st.Get(context.Background(), id)
	mySeat, _ := rec.SeatFor(token)

	advance(srv.timings.Move + time.Second)
	srv.sweepClock(context.Background(), id)

	rec, _ = srv.st.Get(context.Background(), id)
	if !rec.State.IsOut(mySeat.Seat) {
		t.Fatalf("the slow player should be out")
	}
	if rec.Status == "finished" {
		t.Fatalf("three computer players are left; the game should carry on")
	}
	if len(rec.State.Active()) != 3 {
		t.Fatalf("expected three players still in, got %d", len(rec.State.Active()))
	}
}

func TestClockCanBeTurnedOff(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	srv.timings.Move = 0 // KINGSREACH_MOVE_SECONDS=0
	id, token := twoPlayerGame(t, ts)

	rec, _ := srv.st.Get(context.Background(), id)
	mySeat, _ := rec.SeatFor(token)

	advance(24 * time.Hour)
	srv.sweepClock(context.Background(), id)

	rec, _ = srv.st.Get(context.Background(), id)
	if rec.State.IsOut(mySeat.Seat) {
		t.Fatalf("nobody should be timed out with the clock switched off")
	}
}

// One person running every seat has nobody to stall.
func TestOfflineGamesAreExemptFromTheClock(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "offline", "players": 2})
	id, token := g["gameId"].(string), g["token"].(string)

	advance(srv.timings.Move * 20)
	srv.sweepClock(context.Background(), id)

	rec, _ := srv.st.Get(context.Background(), id)
	if len(rec.State.Out) != 0 {
		t.Fatalf("an offline table should never time anybody out: %v", rec.State.Out)
	}
	_ = token
}

// The deadline has to reach the client, or the countdown is a trap.
func TestDeadlineIsReportedToTheClient(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	atTime(srv)
	id, token := twoPlayerGame(t, ts)

	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+token, nil)
	deadline, _ := st["deadline"].(string)
	if deadline == "" {
		t.Fatalf("no deadline in the state: %v", st)
	}
	at, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		t.Fatalf("deadline %q is not a timestamp: %v", deadline, err)
	}
	if !at.After(srv.now()) {
		t.Fatalf("deadline %v is already in the past", at)
	}
}
