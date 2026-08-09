package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"
)

// Friends, invitations, several tables at once, and the boot button.

func listOf(t *testing.T, res map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := res[key].([]any)
	if !ok {
		t.Fatalf("response has no %q list: %v", key, res)
	}
	out := []map[string]any{}
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

// stubFriends replaces the call to Steam for the length of one test.
func stubFriends(t *testing.T, ids []string, err error) {
	t.Helper()
	prev := steamFriendIDs
	steamFriendIDs = func(ctx context.Context, key, steamID string) ([]string, error) {
		return ids, err
	}
	prevKey := SteamKey
	SteamKey = "test-key"
	t.Cleanup(func() { steamFriendIDs, SteamKey = prev, prevKey })
}

// The point of the whole feature: Steam says who your friends are, and we show
// the ones who have actually been here. A friend list of a thousand Steam
// contacts is not a list of people you can play with.
func TestFriendsAreSteamFriendsWhoHavePlayedHere(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	signedIn(t, ts, "2000", "Plays Kingsreach")
	stubFriends(t, []string{"2000", "3000" /* on Steam, never been here */}, nil)

	code, res := request(t, "GET", ts.URL+"/api/friends?token="+me, nil)
	if code != 200 {
		t.Fatalf("friends: %d %v", code, res)
	}
	friends := listOf(t, res, "friends")
	if len(friends) != 1 {
		t.Fatalf("got %d friends, want only the one with a profile: %v", len(friends), friends)
	}
	if friends[0]["name"] != "Plays Kingsreach" {
		t.Fatalf("wrong friend: %v", friends[0])
	}
	// The total is the Steam list, so the UI can say "1 of 2 play Kingsreach"
	// rather than implying you have one friend.
	if total, _ := res["total"].(float64); int(total) != 2 {
		t.Fatalf("total = %v, want 2", res["total"])
	}
}

func TestYouAreNotYourOwnFriend(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	stubFriends(t, []string{"1000"}, nil) // Steam occasionally does this
	_, res := request(t, "GET", ts.URL+"/api/friends?token="+me, nil)
	if got := listOf(t, res, "friends"); len(got) != 0 {
		t.Fatalf("listed myself as a friend: %v", got)
	}
}

// Three ways this can be empty that are not the same thing, and each should
// say which one it is rather than showing a blank list.
func TestFriendsExplainsWhyItIsEmpty(t *testing.T) {
	ts := testServer(t)

	guest := guestToken(t, ts)
	_, res := request(t, "GET", ts.URL+"/api/friends?token="+guest, nil)
	if res["reason"] == nil {
		t.Fatal("a guest gets no explanation for having no friend list")
	}

	me := signedIn(t, ts, "1000", "Me")
	SteamKey = "" // no key configured
	_, res = request(t, "GET", ts.URL+"/api/friends?token="+me, nil)
	if res["reason"] == nil {
		t.Fatal("a server without a Steam key gives no explanation")
	}

	stubFriends(t, nil, errPrivateFriends)
	_, res = request(t, "GET", ts.URL+"/api/friends?token="+me, nil)
	reason, _ := res["reason"].(string)
	if reason == "" || !contains(reason, "private") {
		t.Fatalf("a private friend list should say so, got %q", reason)
	}
}

func TestFriendListIsCachedPerPlayer(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	calls := 0
	prev := steamFriendIDs
	steamFriendIDs = func(ctx context.Context, key, steamID string) ([]string, error) {
		calls++
		return []string{"2000"}, nil
	}
	prevKey := SteamKey
	SteamKey = "test-key"
	t.Cleanup(func() { steamFriendIDs, SteamKey = prev, prevKey })

	for i := 0; i < 3; i++ {
		request(t, "GET", ts.URL+"/api/friends?token="+me, nil)
	}
	if calls != 1 {
		t.Fatalf("asked Steam %d times for three requests; the cache is not working", calls)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func guestToken(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	code, res := request(t, "POST", ts.URL+"/api/profile", map[string]string{"name": "Guest"})
	if code != 200 {
		t.Fatalf("create guest: %d %v", code, res)
	}
	return res["token"].(string)
}

// hostTable opens an online table for a profile and returns its id.
func hostTable(t *testing.T, ts *httptest.Server, profile, name string, players int) string {
	t.Helper()
	code, res := request(t, "POST", ts.URL+"/api/games", map[string]any{
		"mode": "online", "players": players, "profile": profile, "name": name,
	})
	if code != 200 {
		t.Fatalf("create table: %d %v", code, res)
	}
	return res["gameId"].(string)
}

func TestInviteFlow(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "1000", "Host")
	guest := signedIn(t, ts, "2000", "Guest")
	const guestID = "id-2000" // signedIn derives the id from the SteamID

	id := hostTable(t, ts, host, "come and play", 2)

	code, res := request(t, "POST", ts.URL+"/api/invites",
		map[string]string{"token": host, "gameId": id, "profile": guestID})
	if code != 200 {
		t.Fatalf("invite: %d %v", code, res)
	}

	code, res = request(t, "GET", ts.URL+"/api/invites?token="+guest, nil)
	if code != 200 {
		t.Fatalf("list invites: %d %v", code, res)
	}
	invites := listOf(t, res, "invites")
	if len(invites) != 1 {
		t.Fatalf("got %d invitations, want 1: %v", len(invites), invites)
	}
	if invites[0]["table"] != "come and play" || invites[0]["from"] != "Host" {
		t.Fatalf("invitation reads wrong: %v", invites[0])
	}

	// Asking twice is one invitation, not two.
	request(t, "POST", ts.URL+"/api/invites",
		map[string]string{"token": host, "gameId": id, "profile": guestID})
	_, res = request(t, "GET", ts.URL+"/api/invites?token="+guest, nil)
	if got := listOf(t, res, "invites"); len(got) != 1 {
		t.Fatalf("inviting twice produced %d invitations", len(got))
	}

	// Dismissing removes it.
	code, _ = request(t, "POST", ts.URL+"/api/invites/dismiss",
		map[string]string{"token": guest, "id": invites[0]["id"].(string)})
	if code != 200 {
		t.Fatalf("dismiss: %d", code)
	}
	_, res = request(t, "GET", ts.URL+"/api/invites?token="+guest, nil)
	if got := listOf(t, res, "invites"); len(got) != 0 {
		t.Fatalf("dismissed invitation is still there: %v", got)
	}
}

// The endpoint is a way to put a message in front of another player, so only
// somebody actually at the table may use it.
func TestOnlySomeoneAtTheTableMayInvite(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "1000", "Host")
	outsider := signedIn(t, ts, "3000", "Outsider")
	id := hostTable(t, ts, host, "private business", 2)

	code, _ := request(t, "POST", ts.URL+"/api/invites",
		map[string]string{"token": outsider, "gameId": id, "profile": "id-2000"})
	if code != 403 {
		t.Fatalf("outsider invite = %d, want 403", code)
	}
}

func TestInviteToAFullTableIsRefused(t *testing.T) {
	ts := testServer(t)
	host := signedIn(t, ts, "1000", "Host")
	other := signedIn(t, ts, "2000", "Other")
	id := hostTable(t, ts, host, "two seats", 2)
	code, res := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": other})
	if code != 200 {
		t.Fatalf("join: %d %v", code, res)
	}
	code, _ = request(t, "POST", ts.URL+"/api/invites",
		map[string]string{"token": host, "gameId": id, "profile": "id-3000"})
	if code != 409 {
		t.Fatalf("invite to a full table = %d, want 409", code)
	}
}

func TestMyTablesListsEveryGameYouAreIn(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	a := hostTable(t, ts, me, "first table", 2)
	b := hostTable(t, ts, me, "second table", 2)

	code, res := request(t, "GET", ts.URL+"/api/tables?token="+me, nil)
	if code != 200 {
		t.Fatalf("tables: %d %v", code, res)
	}
	tables := listOf(t, res, "tables")
	if len(tables) != 2 {
		t.Fatalf("got %d tables, want 2: %v", len(tables), tables)
	}
	seen := map[string]bool{}
	for _, tbl := range tables {
		seen[tbl["gameId"].(string)] = true
		// The seat token comes back so the client can walk straight back in.
		if tbl["token"] == "" || tbl["token"] == nil {
			t.Fatalf("table %v has no seat token", tbl)
		}
	}
	if !seen[a] || !seen[b] {
		t.Fatalf("tables %v do not include both %s and %s", seen, a, b)
	}
}

func TestMyTablesDropsFinishedGames(t *testing.T) {
	ts := testServer(t)
	me := signedIn(t, ts, "1000", "Me")
	other := signedIn(t, ts, "2000", "Other")
	id := hostTable(t, ts, me, "one and done", 2)
	code, res := request(t, "POST", ts.URL+"/api/games/join",
		map[string]string{"gameId": id, "profile": other})
	if code != 200 {
		t.Fatalf("join: %d %v", code, res)
	}
	myToken := myTokenFor(t, ts, id, "id-1000")
	request(t, "POST", ts.URL+"/api/games/"+id+"/resign", map[string]string{"token": myToken})

	_, res = request(t, "GET", ts.URL+"/api/tables?token="+me, nil)
	if got := listOf(t, res, "tables"); len(got) != 0 {
		t.Fatalf("a finished game is still on the list: %v", got)
	}
}

func myTokenFor(t *testing.T, ts *httptest.Server, gameID, profileID string) string {
	t.Helper()
	srv := ts.Config.Handler.(*Server)
	rec, err := srv.st.Get(context.Background(), gameID)
	if err != nil {
		t.Fatalf("get game: %v", err)
	}
	seat, ok := rec.SeatForProfile(profileID)
	if !ok {
		t.Fatalf("%s holds no seat at %s", profileID, gameID)
	}
	return seat.Token
}
