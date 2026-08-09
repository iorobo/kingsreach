package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

// The boot button, and the rule that keeps it from being a grief tool: you can
// only throw somebody out whose time has *already* run out.

// stalledDuel seats two players, gets past the opening throws, and returns the
// table with one side's clock running.
func stalledDuel(t *testing.T, ts *httptest.Server) (id, mine, theirs, theirSeat string) {
	t.Helper()
	host := signedIn(t, ts, "1000", "Host")
	other := signedIn(t, ts, "2000", "Other")
	id, a, b := duelTable(t, ts, host, other, "somebody walked off")
	rollAll(t, ts, id, a, b)

	// Ask with a known token, so "you" means something: whoever is on the move
	// is the one being waited on, and the other one holds the boot button.
	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+a, nil)
	turn, _ := st["turn"].(string)
	if st["you"] == turn {
		return id, b, a, turn
	}
	return id, a, b, turn
}

func TestBootRefusedWhileTheClockIsStillRunning(t *testing.T) {
	ts := testServer(t)
	id, mine, _, theirSeat := stalledDuel(t, ts)
	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/boot",
		map[string]string{"token": mine, "seat": theirSeat})
	if code != 409 {
		t.Fatalf("boot before the clock ran out = %d, want 409: %v", code, res)
	}
}

func TestBootWorksOnceTheClockHasRunOut(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, mine, _, theirSeat := stalledDuel(t, ts)

	// The clock was armed against the real clock before atTime pinned it, so
	// push well past any plausible deadline.
	advance(2 * srv.timings.Move)
	advance(10 * time.Minute)

	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/boot",
		map[string]string{"token": mine, "seat": theirSeat})
	if code != 200 {
		t.Fatalf("boot after the clock ran out = %d: %v", code, res)
	}
	if res["status"] != "finished" {
		t.Fatalf("booting the only opponent should end the game, status=%v", res["status"])
	}
	for _, s := range seatsOf(res) {
		if s["seat"] == theirSeat && s["cause"] != "out-of-time" {
			t.Fatalf("booted seat says %v, want out-of-time", s["cause"])
		}
	}
}

func TestYouCannotBootYourself(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, mine, _, _ := stalledDuel(t, ts)
	advance(2*srv.timings.Move + 10*time.Minute)

	// Find my own seat name.
	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+mine, nil)
	me, _ := st["you"].(string)
	code, _ := request(t, "POST", ts.URL+"/api/games/"+id+"/boot",
		map[string]string{"token": mine, "seat": me})
	if code != 400 {
		t.Fatalf("booting yourself = %d, want 400", code)
	}
}

func TestOverdueIsReportedOnTheSeat(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, mine, _, theirSeat := stalledDuel(t, ts)

	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+mine, nil)
	if seatNamed(seatsOf(st), theirSeat)["overdue"] == true {
		t.Fatal("seat reported overdue while its clock was still running")
	}
	advance(2*srv.timings.Move + 10*time.Minute)
	_, st = request(t, "GET", ts.URL+"/api/games/"+id+"?token="+mine, nil)
	if seatNamed(seatsOf(st), theirSeat)["overdue"] != true {
		t.Fatalf("seat %s is out of time but not marked overdue: %v", theirSeat, seatsOf(st))
	}
}
