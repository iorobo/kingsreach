package httpapi

import (
	"context"
	"testing"

	"kingsreach/internal/store"
)

// ranked puts a player straight on the board with a given record.
func ranked(t *testing.T, srv *Server, id, name string, wins, played int) *store.Profile {
	t.Helper()
	p := &store.Profile{
		ID: id, Token: "tok-" + id, Kind: KindSteam, SteamID: "steam-" + id, Name: name,
		EquippedSkin: DefaultSkin, EquippedEnv: DefaultEnv,
	}
	if err := srv.st.CreateProfile(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < played; i++ {
		if err := srv.st.BumpProfileStats(context.Background(), p.ID, i < wins); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestLeaderboardNeedsTwentyWins(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)

	ranked(t, srv, "a", "Champion", 25, 30)
	ranked(t, srv, "b", "Contender", 20, 40) // exactly on the line
	almost := ranked(t, srv, "c", "Nearly", 19, 25)

	code, data := request(t, "GET", ts.URL+"/api/leaderboard", nil)
	if code != 200 {
		t.Fatalf("leaderboard status = %d", code)
	}
	if int(data["minWins"].(float64)) != LeaderboardMinWins {
		t.Fatalf("minWins = %v, want %d", data["minWins"], LeaderboardMinWins)
	}
	entries := lobbiesOf(data, "entries")
	if len(entries) != 2 {
		t.Fatalf("expected the two qualified players, got %d: %v", len(entries), entries)
	}
	// Most wins first; a tie breaks on fewer games played.
	if entries[0]["name"] != "Champion" || entries[1]["name"] != "Contender" {
		t.Fatalf("wrong order: %v", entries)
	}
	if int(entries[0]["rank"].(float64)) != 1 || int(entries[1]["rank"].(float64)) != 2 {
		t.Fatalf("ranks not numbered from one: %v", entries)
	}
	for _, e := range entries {
		if e["name"] == "Nearly" {
			t.Fatalf("19 wins must not appear at all: %v", e)
		}
	}

	// Someone short of the line is told how far they have to go.
	_, mine := request(t, "GET", ts.URL+"/api/leaderboard?token="+almost.Token, nil)
	if int(mine["winsNeeded"].(float64)) != 1 {
		t.Fatalf("winsNeeded = %v, want 1", mine["winsNeeded"])
	}
	you := mine["you"].(map[string]any)
	if you["name"] != "Nearly" || int(you["wins"].(float64)) != 19 {
		t.Fatalf("own record wrong: %v", you)
	}
	if _, ranked := you["rank"]; ranked {
		t.Fatalf("an unqualified player has no rank: %v", you)
	}
}

// A player on the board is marked, so they can find themselves in the list.
func TestLeaderboardMarksYou(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	ranked(t, srv, "a", "Champion", 25, 30)
	me := ranked(t, srv, "b", "Me", 22, 30)

	_, data := request(t, "GET", ts.URL+"/api/leaderboard?token="+me.Token, nil)
	entries := lobbiesOf(data, "entries")
	if len(entries) != 2 || entries[1]["you"] != true {
		t.Fatalf("own entry not marked: %v", entries)
	}
	if entries[0]["you"] == true {
		t.Fatalf("somebody else was marked as you: %v", entries[0])
	}
	if int(data["winsNeeded"].(float64)) != 0 {
		t.Fatalf("already on the board, winsNeeded should be 0: %v", data["winsNeeded"])
	}
	you := data["you"].(map[string]any)
	if int(you["rank"].(float64)) != 2 {
		t.Fatalf("own rank = %v, want 2", you["rank"])
	}
}

// Guests keep no progress, so they can never be ranked however much they play.
func TestGuestsNeverRank(t *testing.T) {
	ts := testServer(t)
	srv := ts.Config.Handler.(*Server)
	_, guest := request(t, "POST", ts.URL+"/api/profile", map[string]string{"name": "Passing through"})
	prof, err := srv.st.GetProfileByToken(context.Background(), guest["token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		_ = srv.st.BumpProfileStats(context.Background(), prof.ID, true)
	}
	_, data := request(t, "GET", ts.URL+"/api/leaderboard?token="+prof.Token, nil)
	if n := len(lobbiesOf(data, "entries")); n != 0 {
		t.Fatalf("a guest reached the board: %d entries", n)
	}
	// And they are not told to keep grinding for a place they cannot take.
	if int(data["winsNeeded"].(float64)) != 0 {
		t.Fatalf("a guest should not be given a target: %v", data["winsNeeded"])
	}
}
