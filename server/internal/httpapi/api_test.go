package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kingsreach/internal/store"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(New(store.NewMemory(), t.TempDir()))
	t.Cleanup(ts.Close)
	return ts
}

func request(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer res.Body.Close()
	data := map[string]any{}
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	return res.StatusCode, data
}

func seatsOf(st map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, s := range st["seats"].([]any) {
		out = append(out, s.(map[string]any))
	}
	return out
}

// rollOut throws for every seat that still owes a die, as the client does when
// the player clicks. Returns the state once the opening is settled.
func rollOut(t *testing.T, ts *httptest.Server, id, token string) map[string]any {
	t.Helper()
	var st map[string]any
	for i := 0; i < 40; i++ {
		code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/roll", map[string]string{"token": token})
		if code != 200 {
			t.Fatalf("roll: %d %v", code, res)
		}
		st = res
		if st["phase"] == "play" {
			return st
		}
	}
	t.Fatalf("the opening roll never settled: %v", st)
	return st
}

func TestBoardEndpoint(t *testing.T) {
	ts := testServer(t)
	code, data := request(t, "GET", ts.URL+"/api/board", nil)
	if code != 200 {
		t.Fatalf("board status = %d", code)
	}
	nodes := data["nodes"].([]any)
	edges := data["edges"].([]any)
	if len(nodes) != 55 || len(edges) != 78 {
		t.Fatalf("board = %d nodes / %d edges, want 55/78", len(nodes), len(edges))
	}
}

func TestPracticeFlow(t *testing.T) {
	ts := testServer(t)
	code, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "practice"})
	if code != 200 {
		t.Fatalf("create status = %d (%v)", code, g)
	}
	token, id := g["token"].(string), g["gameId"].(string)
	if g["you"] != "all" || g["status"] != "active" {
		t.Fatalf("practice game should start active with the whole table: %v", g)
	}
	if g["phase"] != "roll" || g["yourRoll"] != true {
		t.Fatalf("a new game waits for the players to throw: %v", g)
	}
	// Nothing moves until the dice are done.
	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/move",
		map[string]string{"token": token, "from": "-4,0", "to": "-1,1"})
	if code != 409 {
		t.Fatalf("moving during the roll = %d, want 409 (%v)", code, res)
	}

	st := rollOut(t, ts, id, token)
	g["turn"] = st["turn"]
	from := "-4,0"
	if st["turn"] == "east" {
		from = "4,0"
	}
	code, mv := request(t, "GET", fmt.Sprintf("%s/api/games/%s/moves?token=%s&from=%s", ts.URL, id, token, from), nil)
	if code != 200 {
		t.Fatalf("moves status = %d", code)
	}
	moves := mv["moves"].([]any)
	if len(moves) == 0 {
		t.Fatalf("expected legal moves for the inner 2 at setup")
	}
	to := moves[0].(map[string]any)["to"].(string)

	code, st = request(t, "POST", ts.URL+"/api/games/"+id+"/move",
		map[string]string{"token": token, "from": from, "to": to})
	if code != 200 {
		t.Fatalf("move status = %d (%v)", code, st)
	}
	if int(st["ply"].(float64)) != 1 || st["lastMove"] == nil {
		t.Fatalf("move not applied: %v", st)
	}

	// Version-aware polling: unchanged -> 204.
	v := int(st["version"].(float64))
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/games/%s?token=%s&v=%d", ts.URL, id, token, v), nil)
	poll, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	poll.Body.Close()
	if poll.StatusCode != 204 {
		t.Fatalf("poll with current version = %d, want 204", poll.StatusCode)
	}

	code, _ = request(t, "POST", ts.URL+"/api/games/"+id+"/move",
		map[string]string{"token": token, "from": to, "to": to})
	if code != 409 {
		t.Fatalf("illegal move status = %d, want 409", code)
	}
	code, _ = request(t, "GET", ts.URL+"/api/games/"+id+"?token=deadbeef", nil)
	if code != 403 {
		t.Fatalf("bad token status = %d, want 403", code)
	}
}

func TestOnlineJoinFlow(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "online"})
	if g["status"] != "waiting" {
		t.Fatalf("online game should start waiting: %v", g)
	}
	if g["you"] != "west" {
		t.Fatalf("creator should hold the first seat, got %v", g["you"])
	}
	joinID := g["gameId"].(string)

	code, j := request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": joinID})
	if code != 200 || j["you"] != "east" || j["status"] != "active" {
		t.Fatalf("join failed: %d %v", code, j)
	}
	code, _ = request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": joinID})
	if code != 409 {
		t.Fatalf("second join status = %d, want 409", code)
	}

	token, id := g["token"].(string), g["gameId"].(string)
	code, st := request(t, "GET", fmt.Sprintf("%s/api/games/%s?token=%s&v=1", ts.URL, id, token), nil)
	if code != 200 || st["status"] != "active" || st["you"] != "west" {
		t.Fatalf("creator poll after join: %d %v", code, st)
	}
}

func TestFourPlayerTable(t *testing.T) {
	ts := testServer(t)
	code, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "online", "players": 4})
	if code != 200 {
		t.Fatalf("create four-player: %d %v", code, g)
	}
	if int(g["players"].(float64)) != 4 || len(seatsOf(g)) != 4 {
		t.Fatalf("expected a four-seat table: %v", g)
	}
	if g["status"] != "waiting" {
		t.Fatalf("table should wait for three more: %v", g["status"])
	}
	joinID := g["gameId"].(string)

	want := []string{"southwest", "east", "northeast"}
	var last map[string]any
	for i, seat := range want {
		code, j := request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": joinID})
		if code != 200 {
			t.Fatalf("join %d: %d %v", i, code, j)
		}
		if j["you"] != seat {
			t.Fatalf("join %d took seat %v, want %s", i, j["you"], seat)
		}
		last = j
	}
	if last["status"] != "active" || last["phase"] != "roll" {
		t.Fatalf("a full table starts by rolling: %v / %v", last["status"], last["phase"])
	}

	// Each player throws their own die; only they may.
	id := g["gameId"].(string)
	seatTokens := map[string]string{g["you"].(string): g["token"].(string)}
	_ = seatTokens
	code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/roll",
		map[string]string{"token": last["token"].(string)})
	if code != 200 {
		t.Fatalf("the last joiner should be able to throw: %d %v", code, res)
	}
	if res["rolled"].(float64) < 1 || res["rolled"].(float64) > 6 {
		t.Fatalf("die out of range: %v", res["rolled"])
	}
	if res["rolledSeat"] != last["you"] {
		t.Fatalf("threw for the wrong seat: %v", res["rolledSeat"])
	}
	code, res = request(t, "POST", ts.URL+"/api/games/"+id+"/roll",
		map[string]string{"token": last["token"].(string)})
	if code != 403 {
		t.Fatalf("throwing twice = %d, want 403 (%v)", code, res)
	}

	// A fifth player finds the table full.
	code, _ = request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": joinID})
	if code != 409 {
		t.Fatalf("fifth join = %d, want 409", code)
	}

	code, _ = request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "online", "players": 5})
	if code != 400 {
		t.Fatalf("five players should be refused, got %d", code)
	}
}

// Practice mode with four seats lets one client drive the whole table.
func TestFourPlayerPractice(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "practice", "players": 4})
	if g["status"] != "active" || g["you"] != "all" {
		t.Fatalf("four-player practice should start immediately: %v", g)
	}
	id, token := g["gameId"].(string), g["token"].(string)
	rollOut(t, ts, id, token)

	// Play one full round and watch the turn travel round the table.
	order := []string{}
	for i := 0; i < 4; i++ {
		_, st := request(t, "GET", fmt.Sprintf("%s/api/games/%s?token=%s", ts.URL, id, token), nil)
		turn := st["turn"].(string)
		order = append(order, turn)
		var from, to string
		for _, p := range st["pieces"].([]any) {
			piece := p.(map[string]any)
			if piece["owner"] != turn || piece["captured"] == true {
				continue
			}
			node := piece["node"].(string)
			_, mv := request(t, "GET",
				fmt.Sprintf("%s/api/games/%s/moves?token=%s&from=%s", ts.URL, id, token, node), nil)
			if opts := mv["moves"].([]any); len(opts) > 0 {
				from, to = node, opts[0].(map[string]any)["to"].(string)
				break
			}
		}
		if from == "" {
			t.Fatalf("%s had no legal move on turn %d", turn, i)
		}
		code, res := request(t, "POST", ts.URL+"/api/games/"+id+"/move",
			map[string]string{"token": token, "from": from, "to": to})
		if code != 200 {
			t.Fatalf("move for %s: %d %v", turn, code, res)
		}
	}
	seen := map[string]bool{}
	for _, s := range order {
		if seen[s] {
			t.Fatalf("seat %s moved twice in one round: %v", s, order)
		}
		seen[s] = true
	}
	if len(seen) != 4 {
		t.Fatalf("a full round should touch all four seats, got %v", order)
	}
}

func TestResignEndpoint(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "practice"})
	id, token := g["gameId"].(string), g["token"].(string)
	code, st := request(t, "POST", ts.URL+"/api/games/"+id+"/resign", map[string]string{"token": token})
	if code != 200 || st["status"] != "finished" || st["winReason"] != "last-standing" {
		t.Fatalf("resign: %d %v", code, st)
	}
	if st["winner"] == g["turn"] {
		t.Fatalf("the seat that resigned must not win (turn=%v winner=%v)", g["turn"], st["winner"])
	}
	for _, seat := range seatsOf(st) {
		if seat["seat"] == g["turn"] && seat["out"] != true {
			t.Fatalf("resigning seat should be marked out: %v", seat)
		}
	}
}

// Resigning one seat of a four-player table leaves the others playing.
func TestResignFourPlayerContinues(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]any{"mode": "practice", "players": 4})
	id, token := g["gameId"].(string), g["token"].(string)
	quitter := g["turn"].(string)

	code, st := request(t, "POST", ts.URL+"/api/games/"+id+"/resign", map[string]string{"token": token})
	if code != 200 {
		t.Fatalf("resign: %d %v", code, st)
	}
	if st["status"] != "active" {
		t.Fatalf("three players remain, game should continue: %v", st["status"])
	}
	if st["turn"] == quitter {
		t.Fatalf("turn should have passed on from %s", quitter)
	}
	outs := 0
	for _, seat := range seatsOf(st) {
		if seat["out"] == true {
			outs++
			if seat["cause"] != "resigned" {
				t.Fatalf("cause = %v, want resigned", seat["cause"])
			}
		}
	}
	if outs != 1 {
		t.Fatalf("exactly one seat should be out, got %d", outs)
	}
}
