package httpapi

import (
	"context"
	"net/url"
	"testing"
	"time"

	"kingsreach/internal/store"
)

func lobbiesOf(st map[string]any, key string) []map[string]any {
	out := []map[string]any{}
	raw, ok := st[key].([]any)
	if !ok {
		return out
	}
	for _, l := range raw {
		out = append(out, l.(map[string]any))
	}
	return out
}

func TestLobbyBrowserIsNeverEmpty(t *testing.T) {
	ts := testServer(t)
	code, res := request(t, "GET", ts.URL+"/api/lobbies", nil)
	if code != 200 {
		t.Fatalf("lobbies status = %d (%v)", code, res)
	}
	open := lobbiesOf(res, "lobbies")
	if len(open) < 1 || len(open) > 4 {
		t.Fatalf("expected one to four open tables, got %d", len(open))
	}
	for _, l := range open {
		if l["gameId"] == "" || l["host"] == "" || l["name"] == "" {
			t.Fatalf("table is missing its details: %v", l)
		}
		if l["country"] == "" {
			t.Fatalf("table has no flag: %v", l)
		}
		if int(l["taken"].(float64)) < 1 {
			t.Fatalf("a table should have its host seated: %v", l)
		}
	}
	running := lobbiesOf(res, "running")
	if len(running) == 0 {
		t.Fatalf("the running list should not be empty")
	}
	// Nobody plays two games at once, and no two tables share a name — either
	// would read as a generator rather than a room.
	seenHost, seenName := map[string]bool{}, map[string]bool{}
	for _, r := range append(running, open...) {
		host, name := r["host"].(string), r["name"].(string)
		if seenHost[host] {
			t.Fatalf("%s appears at two tables at once", host)
		}
		if seenName[name] {
			t.Fatalf("two tables are both called %q", name)
		}
		seenHost[host], seenName[name] = true, true
	}
}

// A table from the browser can actually be joined and played.
func TestJoinLobbyAndPlayTheComputer(t *testing.T) {
	ts := testServer(t)
	_, res := request(t, "GET", ts.URL+"/api/lobbies", nil)
	open := lobbiesOf(res, "lobbies")
	var target map[string]any
	for _, l := range open {
		if l["locked"] != true && int(l["players"].(float64)) == 2 {
			target = l
			break
		}
	}
	if target == nil {
		t.Skip("no open two-player table this run")
	}

	code, joined := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": target["gameId"].(string)})
	if code != 200 {
		t.Fatalf("join failed: %d %v", code, joined)
	}
	if joined["status"] != "active" {
		t.Fatalf("a full table should start: %v", joined["status"])
	}
	// The other seat is played by the computer.
	seats := seatsOf(joined)
	if len(seats) != 2 {
		t.Fatalf("expected two seats, got %d", len(seats))
	}

	// Let the bot take its opening throw and its turns.
	srv := ts.Config.Handler.(*Server)
	id, token := joined["gameId"].(string), joined["token"].(string)
	deadline := time.Now().Add(3 * time.Second)
	rolled := false
	for time.Now().Before(deadline) {
		srv.stepBots(context.Background(), nil)
		_, st := request(t, "GET", ts.URL+"/api/games/"+id+"?token="+token, nil)
		if st["phase"] == "play" {
			rolled = true
			break
		}
		// Our own die still has to be thrown.
		if st["yourRoll"] == true {
			request(t, "POST", ts.URL+"/api/games/"+id+"/roll", map[string]string{"token": token})
		}
	}
	if !rolled {
		t.Fatalf("the opening throws never completed")
	}
}

// Your own table is marked, so the browser can offer "Return" rather than a
// join that would (rightly) be refused.
func TestYourOwnTableIsMarked(t *testing.T) {
	ts := testServer(t)
	tok := signedIn(t, ts, "76561190000000013", "Host")

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "profile": tok, "name": "mine"})
	id := g["gameId"].(string)

	_, mine := request(t, "GET", ts.URL+"/api/lobbies?token="+tok, nil)
	found := false
	for _, l := range lobbiesOf(mine, "lobbies") {
		if l["gameId"] == id {
			found = true
			if l["yours"] != true {
				t.Fatalf("the host's own table should be marked: %v", l)
			}
		}
	}
	if !found {
		t.Fatalf("the table is missing from the browser")
	}

	// To everyone else it is just a table.
	other := signedIn(t, ts, "76561190000000014", "Stranger")
	_, theirs := request(t, "GET", ts.URL+"/api/lobbies?token="+other, nil)
	for _, l := range lobbiesOf(theirs, "lobbies") {
		if l["gameId"] == id && l["yours"] == true {
			t.Fatalf("somebody else's table was marked as theirs: %v", l)
		}
	}
}

func TestLockedLobbyNeedsThePassword(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "name": "Private table", "password": "hunter2"})
	id := g["gameId"].(string)

	_, list := request(t, "GET", ts.URL+"/api/lobbies", nil)
	found := false
	for _, l := range lobbiesOf(list, "lobbies") {
		if l["gameId"] == id {
			found = true
			if l["locked"] != true {
				t.Fatalf("a table with a password should show as locked: %v", l)
			}
			if l["name"] != "Private table" {
				t.Fatalf("table name = %v", l["name"])
			}
		}
	}
	if !found {
		t.Fatalf("the new table is not in the browser")
	}

	code, _ := request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": id})
	if code != 403 {
		t.Fatalf("join without password = %d, want 403", code)
	}
	code, _ = request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "password": "wrong"})
	if code != 403 {
		t.Fatalf("join with the wrong password = %d, want 403", code)
	}
	code, joined := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "password": "hunter2"})
	if code != 200 {
		t.Fatalf("join with the right password = %d (%v)", code, joined)
	}
}

// Starting early hands the empty seats to the computer.
func TestStartEarlyFillsWithComputer(t *testing.T) {
	ts := testServer(t)
	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 4, "name": "Come one come all"})
	id, token := g["gameId"].(string), g["token"].(string)

	code, st := request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})
	if code != 200 {
		t.Fatalf("start: %d %v", code, st)
	}
	if st["status"] != "active" {
		t.Fatalf("table should be active, got %v", st["status"])
	}
	for _, seat := range seatsOf(st) {
		if seat["taken"] != true {
			t.Fatalf("every seat should be filled after starting early: %v", seat)
		}
	}
}

// Guests keep no progress; a signed-in player does.
func TestGuestsKeepNoProgress(t *testing.T) {
	ts := testServer(t)
	_, guest := request(t, "POST", ts.URL+"/api/profile", map[string]string{"name": "Rob"})
	if guest["kind"] != "guest" || guest["persistent"] != false {
		t.Fatalf("a chosen name makes a guest: %v", guest)
	}
	if guest["name"] != "Rob" {
		t.Fatalf("guest name = %v", guest["name"])
	}

	srv := ts.Config.Handler.(*Server)
	prof, err := srv.st.GetProfileByToken(context.Background(), guest["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.st.BumpProfileStats(context.Background(), prof.ID, true); err != nil {
		t.Fatalf("bump: %v", err)
	}
	_, after := request(t, "GET", ts.URL+"/api/profile?token="+guest["token"].(string), nil)
	if int(after["gamesPlayed"].(float64)) != 0 || int(after["wins"].(float64)) != 0 {
		t.Fatalf("a guest must not accumulate progress: %v", after)
	}

	// A Steam identity does.
	steam := &store.Profile{
		ID: "p-steam", Token: "t-steam", Kind: KindSteam, SteamID: "76561190000000000",
		Name: "SteamFriend", EquippedSkin: DefaultSkin, EquippedEnv: DefaultEnv,
	}
	if err := srv.st.CreateProfile(context.Background(), steam); err != nil {
		t.Fatal(err)
	}
	if err := srv.st.BumpProfileStats(context.Background(), steam.ID, true); err != nil {
		t.Fatal(err)
	}
	_, sp := request(t, "GET", ts.URL+"/api/profile?token=t-steam", nil)
	if int(sp["gamesPlayed"].(float64)) != 1 || int(sp["wins"].(float64)) != 1 {
		t.Fatalf("a signed-in player keeps progress: %v", sp)
	}
	if sp["persistent"] != true {
		t.Fatalf("steam profiles are persistent: %v", sp)
	}
}

// The callback is worthless unless Steam itself confirms it.
func TestSteamCallbackMustBeVerified(t *testing.T) {
	auth := &SteamAuth{Realm: "http://localhost:8080"}
	valid := url.Values{"openid.claimed_id": {"https://steamcommunity.com/openid/id/76561197960287930"}}

	auth.Verify = func(context.Context, url.Values) (bool, error) { return false, nil }
	if _, err := auth.SteamIDFromCallback(context.Background(), valid); err == nil {
		t.Fatalf("an unconfirmed callback must be refused")
	}

	auth.Verify = func(context.Context, url.Values) (bool, error) { return true, nil }
	id, err := auth.SteamIDFromCallback(context.Background(), valid)
	if err != nil || id != "76561197960287930" {
		t.Fatalf("verified callback: id=%q err=%v", id, err)
	}

	// A forged identity URL never reaches the verification step.
	forged := url.Values{"openid.claimed_id": {"https://evil.example.com/openid/id/76561197960287930"}}
	if _, err := auth.SteamIDFromCallback(context.Background(), forged); err == nil {
		t.Fatalf("a claimed_id from another host must be refused")
	}
}

func TestSteamSignInCreatesAndReusesProfile(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	srv.steam = &SteamAuth{
		Realm:   ts.URL,
		Verify:  func(context.Context, url.Values) (bool, error) { return true, nil },
		Summary: func(context.Context, string) (string, string, error) { return "Rob", "http://img/av.jpg", nil },
	}

	ret := ts.URL + "/api/auth/steam/return?openid.claimed_id=" +
		url.QueryEscape("https://steamcommunity.com/openid/id/76561197960287930")
	code, _ := request(t, "GET", ret, nil)
	if code != 200 && code != 302 {
		t.Fatalf("steam return status = %d", code)
	}

	prof, err := srv.st.GetProfileBySteamID(context.Background(), "76561197960287930")
	if err != nil {
		t.Fatalf("no profile was created: %v", err)
	}
	if prof.Kind != KindSteam || prof.Name != "Rob" || prof.Avatar == "" {
		t.Fatalf("steam profile not filled in: %+v", prof)
	}

	// Signing in again reuses the same identity rather than making another.
	first := prof.ID
	request(t, "GET", ret, nil)
	again, err := srv.st.GetProfileBySteamID(context.Background(), "76561197960287930")
	if err != nil || again.ID != first {
		t.Fatalf("second sign-in should reuse profile %s, got %+v (%v)", first, again, err)
	}
}
