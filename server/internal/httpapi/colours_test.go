package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
)

// Colour is a preference, and the dice settle the argument.

func seatNamed(seats []map[string]any, seat string) map[string]any {
	for _, s := range seats {
		if s["seat"] == seat {
			return s
		}
	}
	return nil
}

// prefer sets a profile's colour the way the Collection screen does.
func prefer(t *testing.T, ts *httptest.Server, token, colour string) {
	t.Helper()
	code, res := request(t, "POST", ts.URL+"/api/profile/equip",
		map[string]string{"token": token, "colour": colour})
	if code != 200 {
		t.Fatalf("set colour %s: %d %v", colour, code, res)
	}
}

// rollAll throws for several seats until the opening settles.
//
// rollOut in api_test.go only works when one token holds the whole table; here
// each player throws for themselves, and ties mean another round with only the
// leaders in it, so the loop keeps offering every token a throw and lets the
// server refuse the ones that are not owed.
func rollAll(t *testing.T, ts *httptest.Server, id string, tokens ...string) map[string]any {
	t.Helper()
	var st map[string]any
	for i := 0; i < 60; i++ {
		for _, tok := range tokens {
			code, r := request(t, "POST", ts.URL+"/api/games/"+id+"/roll",
				map[string]string{"token": tok})
			if code == 200 {
				st = r
			}
		}
		if st != nil && st["phase"] == "play" {
			return st
		}
	}
	t.Fatalf("the opening never settled: %v", st)
	return st
}

// duelTable seats two signed-in players at a fresh table and returns its id
// and their seat tokens.
func duelTable(t *testing.T, ts *httptest.Server, hostTok, otherTok, name string) (string, string, string) {
	t.Helper()
	id := hostTable(t, ts, hostTok, name, 2)
	code, res := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": otherTok})
	if code != 200 {
		t.Fatalf("join: %d %v", code, res)
	}
	return id, myTokenFor(t, ts, id, "id-1000"), myTokenFor(t, ts, id, "id-2000")
}

func TestSeatKeepsItsOwnColourWhenNobodyAsks(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	other := signedIn(t, ts, "2000", "Other")
	id, a, b := duelTable(t, ts, me, other, "no preferences")
	seats := seatsOf(rollAll(t, ts, id, a, b))
	if got := seatNamed(seats, "west")["colour"]; got != "obsidian" {
		t.Fatalf("west is wearing %v, want its own obsidian", got)
	}
	if got := seatNamed(seats, "east")["colour"]; got != "ember" {
		t.Fatalf("east is wearing %v, want its own ember", got)
	}
}

// The point of the feature: sit anywhere, play the colour you like.
func TestYouGetTheColourYouAskedFor(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	other := signedIn(t, ts, "2000", "Other")
	prefer(t, ts, me, "jade")
	id, a, b := duelTable(t, ts, me, other, "green please")
	st := rollAll(t, ts, id, a, b)
	if got := seatNamed(seatsOf(st), "west")["colour"]; got != "jade" {
		t.Fatalf("west asked for jade and got %v", got)
	}
}

// Two players, one colour: whoever wins the opening throw gets it, and the
// other falls back to their seat's own rather than to nothing.
func TestContestedColourGoesToTheDiceWinner(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "1000", "Host")
	other := signedIn(t, ts, "2000", "Other")
	prefer(t, ts, host, "azure")
	prefer(t, ts, other, "azure")

	id, a, b := duelTable(t, ts, host, other, "both want blue")
	st := rollAll(t, ts, id, a, b)
	seats := seatsOf(st)
	winner, _ := st["turn"].(string)
	loser := "west"
	if winner == "west" {
		loser = "east"
	}
	if got := seatNamed(seats, winner)["colour"]; got != "azure" {
		t.Fatalf("the dice winner (%s) got %v, not the azure both asked for", winner, got)
	}
	// The loser keeps a colour — their seat's own — rather than being left blank.
	other2 := seatNamed(seats, loser)["colour"]
	if other2 == "azure" || other2 == "" || other2 == nil {
		t.Fatalf("the other seat (%s) ended up with %v", loser, other2)
	}
}

// Colours are handed out once. A later poll must not reshuffle them, or the
// pieces change colour under the players mid-game.
func TestColoursAreSettledOnce(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	other := signedIn(t, ts, "2000", "Other")
	prefer(t, ts, me, "plum")
	id, a, b := duelTable(t, ts, me, other, "stable colours")
	first := rollAll(t, ts, id, a, b)

	// Change the preference afterwards; the running game must not notice.
	prefer(t, ts, me, "amber")
	_, again := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+a, nil)
	if a, b := seatNamed(seatsOf(first), "west")["colour"], seatNamed(seatsOf(again), "west")["colour"]; a != b {
		t.Fatalf("colour changed mid-game from %v to %v", a, b)
	}
}

func TestUnknownColourIsRefused(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	code, _ := request(t, "POST", ts.URL+"/api/profile/equip",
		map[string]string{"token": me, "colour": "chartreuse"})
	if code != 400 {
		t.Fatalf("unknown colour = %d, want 400", code)
	}
}

func TestColoursEndpointListsSix(t *testing.T) {
	ts := testServer(t)
	code, res := request(t, "GET", ts.URL+"/api/colours", nil)
	if code != 200 {
		t.Fatalf("colours: %d", code)
	}
	if got := listOf(t, res, "colours"); len(got) != 6 {
		t.Fatalf("got %d colours, want 6", len(got))
	}
}

// Offline the host plays every seat, so one preference cannot speak for all of
// them — every seat keeps its own colour.
func TestOfflineTableKeepsSeatColours(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	prefer(t, ts, me, "jade")
	code, res := request(t, "POST", ts.URL+"/api/games", map[string]any{
		"mode": "offline", "players": 4, "profile": me,
	})
	if code != 200 {
		t.Fatalf("create: %d %v", code, res)
	}
	id := res["gameId"].(string)
	st := rollOut(t, ts, id, res["token"].(string))
	seen := map[string]bool{}
	for _, s := range seatsOf(st) {
		c, _ := s["colour"].(string)
		if c == "" {
			t.Fatalf("seat %v has no colour", s["seat"])
		}
		if seen[c] {
			t.Fatalf("two seats are wearing %s", c)
		}
		seen[c] = true
	}
}

// A game saved before colours existed has none on its seats, and must still
// render — the seat's own colour is the fallback.
func TestOldGamesFallBackToTheSeatColour(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	id := hostTable(t, ts, me, "an old row", 2)
	srv := ts.Config.Handler.(*Server)
	rec, err := srv.st.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for i := range rec.Seats {
		rec.Seats[i].Colour = "" // as a row written before this existed
	}
	if err := srv.st.Update(context.Background(), rec); err != nil {
		t.Fatalf("update: %v", err)
	}
	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+myTokenFor(t, ts, id, "id-1000"), nil)
	if got := seatNamed(seatsOf(st), "west")["colour"]; got != "obsidian" {
		t.Fatalf("a colourless old seat rendered as %v, want its seat colour", got)
	}
}
