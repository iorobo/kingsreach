package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"kingsreach/internal/store"
)

func TestCatalogEndpoint(t *testing.T) {
	ts := testServer(t)
	code, data := request(t, "GET", ts.URL+"/api/catalog", nil)
	if code != 200 {
		t.Fatalf("catalog status = %d", code)
	}
	items := data["items"].([]any)
	if len(items) != len(catalog) {
		t.Fatalf("catalog items = %d, want %d", len(items), len(catalog))
	}
}

func TestDevUnlockAll(t *testing.T) {
	UnlockAll = true
	t.Cleanup(func() { UnlockAll = false })

	ts := testServer(t)
	_, p := request(t, "POST", ts.URL+"/api/profile", nil)
	token := p["token"].(string)
	if p["devUnlockAll"] != true {
		t.Fatalf("devUnlockAll should be reported to the client: %v", p)
	}
	if got := len(p["unlocked"].([]any)); got != len(catalog) {
		t.Fatalf("unlocked = %d items, want all %d", got, len(catalog))
	}
	// A brand-new profile can equip anything, including the costliest item.
	for _, id := range []string{"rune", "cafe"} {
		kind := "skin"
		if itemByID(id).Kind == "env" {
			kind = "env"
		}
		body := map[string]string{"token": token}
		body[kind] = id
		code, res := request(t, "POST", ts.URL+"/api/profile/equip", body)
		if code != 200 {
			t.Fatalf("equip %s in dev mode: status %d (%v)", id, code, res)
		}
	}

	// With the flag off again the gate is back in place.
	UnlockAll = false
	_, fresh := request(t, "POST", ts.URL+"/api/profile", nil)
	if got := len(fresh["unlocked"].([]any)); got != 2 {
		t.Fatalf("unlocked without dev mode = %d, want 2 defaults", got)
	}
	code, _ := request(t, "POST", ts.URL+"/api/profile/equip",
		map[string]string{"token": fresh["token"].(string), "skin": "rune"})
	if code != 409 {
		t.Fatalf("equip locked skin without dev mode = %d, want 409", code)
	}
}

// signedIn makes a Steam-backed identity — only those keep progress, so the
// progression tests cannot use guests.
func signedIn(t *testing.T, ts *httptest.Server, steamID, name string) string {
	t.Helper()
	srv := ts.Config.Handler.(*Server)
	token := "tok-" + steamID
	p := &store.Profile{
		ID: "id-" + steamID, Token: token, Kind: KindSteam, SteamID: steamID, Name: name,
		EquippedSkin: DefaultSkin, EquippedEnv: DefaultEnv,
	}
	if err := srv.st.CreateProfile(context.Background(), p); err != nil {
		t.Fatalf("could not create signed-in profile: %v", err)
	}
	return token
}

func TestProfileProgressionAndEquip(t *testing.T) {
	ts := testServer(t)

	tokA := signedIn(t, ts, "76561190000000001", "Ada")
	tokB := signedIn(t, ts, "76561190000000002", "Bo")
	_, a := request(t, "GET", ts.URL+"/api/profile?token="+tokA, nil)
	if a["equippedSkin"] != "clay" || a["equippedEnv"] != "picnic" {
		t.Fatalf("fresh profile defaults wrong: %v", a)
	}

	// Fresh profiles cannot equip locked items.
	code, _ := request(t, "POST", ts.URL+"/api/profile/equip", map[string]string{"token": tokA, "skin": "royal"})
	if code != 409 {
		t.Fatalf("equip locked skin status = %d, want 409", code)
	}

	// Play an online game between the two profiles; the mover resigns, so
	// the opponent wins and both count one played game.
	_, g := request(t, "POST", ts.URL+"/api/games", map[string]string{"mode": "online", "profile": tokA})
	_, j := request(t, "POST", ts.URL+"/api/games/join", map[string]string{"gameId": g["gameId"].(string), "profile": tokB})
	gameID := g["gameId"].(string)
	for _, seat := range seatsOf(j) {
		if seat["skin"] != "clay" {
			t.Fatalf("each seat snapshots the skin it sat down with: %v", seat)
		}
	}

	// West (creator, profile A) resigns; east (profile B) wins.
	code, st := request(t, "POST", ts.URL+"/api/games/"+gameID+"/resign", map[string]string{"token": g["token"].(string)})
	if code != 200 || st["winner"] != "east" {
		t.Fatalf("resign flow: %d %v", code, st)
	}

	_, a2 := request(t, "GET", fmt.Sprintf("%s/api/profile?token=%s", ts.URL, tokA), nil)
	_, b2 := request(t, "GET", fmt.Sprintf("%s/api/profile?token=%s", ts.URL, tokB), nil)
	if int(a2["gamesPlayed"].(float64)) != 1 || int(a2["wins"].(float64)) != 0 {
		t.Fatalf("loser stats wrong: %v", a2)
	}
	if int(b2["gamesPlayed"].(float64)) != 1 || int(b2["wins"].(float64)) != 1 {
		t.Fatalf("winner stats wrong: %v", b2)
	}

	// One win unlocks Royal Gold (and nothing costlier); the winner equips it.
	unlocked := b2["unlocked"].([]any)
	found := false
	for _, u := range unlocked {
		if u == "royal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("royal should be unlocked after 1 win: %v", unlocked)
	}
	code, b3 := request(t, "POST", ts.URL+"/api/profile/equip", map[string]string{"token": tokB, "skin": "royal"})
	if code != 200 || b3["equippedSkin"] != "royal" {
		t.Fatalf("equip unlocked skin failed: %d %v", code, b3)
	}
	// The loser still cannot.
	code, _ = request(t, "POST", ts.URL+"/api/profile/equip", map[string]string{"token": tokA, "skin": "royal"})
	if code != 409 {
		t.Fatalf("loser equip status = %d, want 409", code)
	}

	// New game snapshots the newly equipped skin onto the creator's seat.
	_, g2 := request(t, "POST", ts.URL+"/api/games", map[string]string{"mode": "online", "profile": tokB})
	if seatsOf(g2)[0]["skin"] != "royal" {
		t.Fatalf("new game should snapshot equipped skin, got %v", seatsOf(g2)[0])
	}

	// Offline is for a group round one screen; it records nothing at all.
	_, p := request(t, "POST", ts.URL+"/api/games", map[string]string{"mode": "offline", "profile": tokA})
	request(t, "POST", ts.URL+"/api/games/"+p["gameId"].(string)+"/resign", map[string]string{"token": p["token"].(string)})
	_, a3 := request(t, "GET", fmt.Sprintf("%s/api/profile?token=%s", ts.URL, tokA), nil)
	if int(a3["gamesPlayed"].(float64)) != 1 || int(a3["wins"].(float64)) != 0 {
		t.Fatalf("offline must record nothing (want played=1 wins=0): %v", a3)
	}
	// The old name still works, and still records nothing.
	_, q := request(t, "POST", ts.URL+"/api/games", map[string]string{"mode": "practice", "profile": tokA})
	if q["mode"] != "offline" {
		t.Fatalf("legacy \"practice\" should normalise to offline, got %v", q["mode"])
	}
}

// Two browser windows on one account must not end up facing each other.
func TestOneAccountCannotTakeTwoSeats(t *testing.T) {
	ts := testServer(t)
	tok := signedIn(t, ts, "76561190000000010", "Twofold")

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 4, "profile": tok, "name": "just me"})
	id := g["gameId"].(string)

	// The creator already holds a seat, so a second window is refused.
	code, res := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": tok})
	if code != 409 {
		t.Fatalf("second seat for the same account = %d, want 409 (%v)", code, res)
	}

	// Somebody else still gets in.
	other := signedIn(t, ts, "76561190000000011", "Rival")
	code, res = request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": other})
	if code != 200 {
		t.Fatalf("a different account should be welcome: %d %v", code, res)
	}
	// And is then refused a second seat too.
	code, _ = request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": other})
	if code != 409 {
		t.Fatalf("rival's second seat = %d, want 409", code)
	}
}

// Even if a duplicate seat got in some other way, it wins nothing.
func TestSelfPlayAwardsNoWin(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	tok := signedIn(t, ts, "76561190000000012", "Mirror")

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 3, "profile": tok, "name": "hall of mirrors"})
	id, token := g["gameId"].(string), g["token"].(string)
	// Start short-handed: the computer fills the rest, which would normally
	// make this a contested game.
	request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})

	rec, err := srv.st.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// Force the state the join check now prevents: the same identity twice.
	me, _ := rec.SeatFor(token)
	for i := range rec.Seats {
		if rec.Seats[i].Seat != me.Seat {
			rec.Seats[i].Bot = false
			rec.Seats[i].Profile = me.Profile
			break
		}
	}
	rec.State.Status, rec.State.Winner, rec.State.WinReason = "finished", me.Seat, "throne"
	rec.Status = "finished"
	if err := srv.st.Update(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	srv.awardStats(context.Background(), rec)

	_, after := request(t, "GET", ts.URL+"/api/profile?token="+tok, nil)
	if int(after["wins"].(float64)) != 0 {
		t.Fatalf("beating yourself is not a victory: %v", after)
	}
	if int(after["gamesPlayed"].(float64)) != 1 {
		t.Fatalf("the game still happened: played = %v, want 1", after["gamesPlayed"])
	}
}

// Beating the computer is a win. This is the case that was silently dropped:
// the old rule needed two human profiles at the table.
func TestWinningAgainstTheComputerCounts(t *testing.T) {
	ts := testServer(t)
	tok := signedIn(t, ts, "76561190000000009", "Solo")

	_, g := request(t, "POST", ts.URL+"/api/games",
		map[string]any{"mode": "online", "players": 2, "profile": tok, "name": "me vs the house"})
	id, token := g["gameId"].(string), g["token"].(string)

	// Start short-handed so the computer takes the other seat.
	code, st := request(t, "POST", ts.URL+"/api/games/"+id+"/start", map[string]string{"token": token})
	if code != 200 {
		t.Fatalf("start: %d %v", code, st)
	}
	// The computer resigns for us by way of the human winning: resign from the
	// bot's seat is not reachable, so drive it from the state instead.
	srv := ts.Config.Handler.(*Server)
	rec, err := srv.st.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	me, ok := rec.SeatFor(token)
	if !ok {
		t.Fatal("we hold no seat")
	}
	rec.State.Status, rec.State.Winner, rec.State.WinReason = "finished", me.Seat, "throne"
	rec.Status = "finished"
	if err := srv.st.Update(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	srv.awardStats(context.Background(), rec)

	_, after := request(t, "GET", ts.URL+"/api/profile?token="+tok, nil)
	if int(after["wins"].(float64)) != 1 {
		t.Fatalf("beating the computer should count as a win: %v", after)
	}
	if int(after["gamesPlayed"].(float64)) != 1 {
		t.Fatalf("games played = %v, want 1", after["gamesPlayed"])
	}
}
