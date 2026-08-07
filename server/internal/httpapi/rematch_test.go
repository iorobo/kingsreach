package httpapi

import (
	"net/http/httptest"
	"testing"
)

// finishedGame plays a table to a conclusion by resigning, and returns the id
// plus both seat tokens.
func finishedGame(t *testing.T, ts *httptest.Server, hostProfile, guestProfile string) (string, string, string) {
	t.Helper()
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "profile": hostProfile, "name": "again?"})
	id, hostToken := g["gameId"].(string), g["token"].(string)
	_, j := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": guestProfile})
	guestToken := j["token"].(string)
	request(t, "POST", ts.URL+"/api/games/"+id+"/resign", map[string]string{"token": hostToken})
	return id, hostToken, guestToken
}

func TestRematchSeatsBothPlayersAtOneNewTable(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "76561190000000030", "Host")
	guest := signedIn(t, ts, "76561190000000031", "Guest")
	id, hostToken, guestToken := finishedGame(t, ts, host, guest)

	code, first := request(t, "POST", ts.URL+"/api/games/"+id+"/rematch",
		map[string]string{"token": hostToken})
	if code != 200 {
		t.Fatalf("rematch: %d %v", code, first)
	}
	newID := first["gameId"].(string)
	if newID == id {
		t.Fatalf("the rematch reused the finished game")
	}

	// The second player asking must land at the same table, not a third one.
	code, second := request(t, "POST", ts.URL+"/api/games/"+id+"/rematch",
		map[string]string{"token": guestToken})
	if code != 200 {
		t.Fatalf("second rematch call: %d %v", code, second)
	}
	if second["gameId"] != newID {
		t.Fatalf("players landed at different tables: %v vs %v", second["gameId"], newID)
	}
	if second["token"] == first["token"] {
		t.Fatalf("both players were handed the same seat token")
	}
	if second["you"] == first["you"] {
		t.Fatalf("both players were given seat %v", second["you"])
	}
	if second["status"] != "active" {
		t.Fatalf("with both seats filled the rematch should be under way: %v", second["status"])
	}

	// The finished game points at the rematch, which is how a player still on
	// the result screen finds out that somebody wants another go.
	_, old := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+guestToken, nil)
	if old["rematchId"] != newID {
		t.Fatalf("the old game does not point at the rematch: %v", old["rematchId"])
	}
}

// A rematch is between the people who just played, not an open invitation.
func TestRematchIsNotAdvertised(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "76561190000000032", "Host")
	guest := signedIn(t, ts, "76561190000000033", "Guest")
	id, hostToken, _ := finishedGame(t, ts, host, guest)

	_, res := request(t, "POST", ts.URL+"/api/games/"+id+"/rematch", map[string]string{"token": hostToken})
	newID := res["gameId"].(string)

	_, list := request(t, "GET", ts.URL+"/api/lobbies", nil)
	for _, l := range lobbiesOf(list, "lobbies") {
		if l["gameId"] == newID {
			t.Fatalf("the rematch turned up in the lobby browser: %v", l)
		}
	}
}

// Against the computer there is nobody to agree with, so it just starts.
func TestRematchAgainstTheComputerStartsAtOnce(t *testing.T) {
	ts := testServer(t)
	tok := signedIn(t, ts, "76561190000000034", "Solo")
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "profile": tok, "name": "me and the house"})
	id, token := g["gameId"].(string), g["token"].(string)
	request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})
	request(t, "POST", ts.URL+"/api/games/"+id+"/resign", map[string]string{"token": token})

	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/rematch", map[string]string{"token": token})
	if code != 200 {
		t.Fatalf("rematch: %d %v", code, res)
	}
	if res["status"] != "active" {
		t.Fatalf("a rematch with the computer should be ready to play: %v", res["status"])
	}
	seats := seatsOf(res)
	bots := 0
	for _, s := range seats {
		if s["bot"] == true {
			bots++
		}
	}
	if bots != 1 {
		t.Fatalf("expected the computer to come along to the rematch, found %d bot seats", bots)
	}
}

func TestRematchNeedsAFinishedGameAndASeat(t *testing.T) {
	ts := testServer(t)
	tok := signedIn(t, ts, "76561190000000035", "Impatient")
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "profile": tok})
	id, token := g["gameId"].(string), g["token"].(string)

	code, _ := request(t, "POST", ts.URL+"/api/games/"+id+"/rematch", map[string]string{"token": token})
	if code != 409 {
		t.Fatalf("rematch of an unfinished game = %d, want 409", code)
	}
	code, _ = request(t, "POST", ts.URL+"/api/games/"+id+"/rematch", map[string]string{"token": "not-a-seat"})
	if code != 403 {
		t.Fatalf("rematch by a stranger = %d, want 403", code)
	}
}
