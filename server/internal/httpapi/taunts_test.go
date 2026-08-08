package httpapi

import (
	"testing"
	"time"
)

// The limiter lives on the server because a disabled button stops nobody.

func TestTauntsAreRateLimited(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "nice-move"})
	if code != 200 {
		t.Fatalf("first taunt: %d %v", code, res)
	}

	// Straight away again: refused.
	code, res = request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "nice-move"})
	if code != 429 {
		t.Fatalf("taunting twice in a row = %d, want 429 (%v)", code, res)
	}

	// After the gap: allowed again.
	advance(tauntGap + time.Second)
	code, _ = request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "nice-game"})
	if code != 200 {
		t.Fatalf("taunt after the gap = %d, want 200", code)
	}
}

// The gap alone would let somebody drip-feed one every eight seconds for a
// whole game, so there is a ceiling as well.
func TestTauntsRunOutOverAGame(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	sent := 0
	for i := 0; i < tauntsPerGame+5; i++ {
		code, _ := request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
			map[string]string{"token": token, "id": "nice-move"})
		if code == 200 {
			sent++
		}
		advance(tauntGap + time.Second)
	}
	if sent != tauntsPerGame {
		t.Fatalf("sent %d taunts, want the ceiling of %d", sent, tauntsPerGame)
	}
}

// A taunt reaches the table through the state, and the version has to move or
// the other player's poll answers "nothing changed" and never sees it.
func TestTauntReachesTheOtherPlayer(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	atTime(srv)
	id, token := twoPlayerGame(t, ts)

	_, before := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+token, nil)
	wasAt := int(before["version"].(float64))

	request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "bow-now"})

	_, after := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+token, nil)
	if int(after["version"].(float64)) <= wasAt {
		t.Fatalf("the version did not move, so a polling opponent would never hear it")
	}
	taunt, ok := after["taunt"].(map[string]any)
	if !ok {
		t.Fatalf("no taunt on the state: %v", after["taunt"])
	}
	if taunt["text"] != "Bow now, save yourself the trouble later" {
		t.Fatalf("wrong words: %v", taunt["text"])
	}
	if taunt["nonce"] == "" {
		t.Fatalf("a taunt needs a nonce, or a repeat looks like a poll echo")
	}

	// A watcher hears it too — they are at the table, just not playing.
	_, watched := request(t, "GET", ts.URL+"/api/games/"+id+"/watch", nil)
	if _, ok := watched["taunt"].(map[string]any); !ok {
		t.Fatalf("a spectator should see the taunt as well")
	}
}

// It goes quiet again by itself, so a reload much later is not greeted by
// something somebody said ten minutes ago.
func TestTauntsExpire(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	advance := atTime(srv)
	id, token := twoPlayerGame(t, ts)

	request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "nice-move"})
	advance(tauntShelfLife + time.Second)

	_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+token, nil)
	if st["taunt"] != nil {
		t.Fatalf("a taunt should have gone quiet by now: %v", st["taunt"])
	}
}

func TestTauntsNeedASeatAndAnAudience(t *testing.T) {
	ts := testServer(t)
	id, token := twoPlayerGame(t, ts)

	code, _ := request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": "not-a-seat", "id": "nice-move"})
	if code != 403 {
		t.Fatalf("a stranger taunting = %d, want 403", code)
	}
	code, _ = request(t, "POST", ts.URL+"/api/games/"+id+"/taunt",
		map[string]string{"token": token, "id": "no-such-line"})
	if code != 400 {
		t.Fatalf("an unknown taunt = %d, want 400", code)
	}

	// Offline is one person at one screen; there is nobody to say it to.
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "offline", "players": 2})
	code, _ = request(t, "POST", ts.URL+"/api/games/"+g["gameId"].(string)+"/taunt",
		map[string]string{"token": g["token"].(string), "id": "nice-move"})
	if code != 409 {
		t.Fatalf("taunting offline = %d, want 409", code)
	}
}

// The catalogue is the single source of both the sound and the words a player
// reads when the sound is off. They must describe the same taunt.
func TestEveryTauntHasWordsAndASound(t *testing.T) {
	ts := testServer(t)
	_, res := request(t, "GET", ts.URL+"/api/taunts", nil)
	list, ok := res["taunts"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("no taunts offered: %v", res)
	}
	seen := map[string]bool{}
	for _, raw := range list {
		it := raw.(map[string]any)
		id, _ := it["id"].(string)
		text, _ := it["text"].(string)
		sound, _ := it["sound"].(string)
		if id == "" || text == "" || sound == "" {
			t.Fatalf("incomplete taunt: %v", it)
		}
		if seen[id] {
			t.Fatalf("duplicate taunt id %q", id)
		}
		seen[id] = true
	}
}
