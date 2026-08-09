package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// Friends, borrowed from Steam.
//
// There is no friend list of our own here, deliberately. Building one means a
// request flow, an accept flow, a block flow and a report flow, all to end up
// with a worse copy of the list the player already has. Steam knows who your
// friends are; this asks it, and then shows you the ones who have signed in to
// Kingsreach at least once. Everyone else on that list is a name we could not
// do anything useful with anyway — you cannot invite someone who has no seat
// to sit in.
//
// Two things this needs that OpenID sign-in did not:
//
//   - A Steam Web API key (KINGSREACH_STEAM_KEY). GetFriendList is one of the
//     endpoints that will not answer without one, unlike the public profile
//     XML the sign-in uses for names and avatars. No key, no friend list —
//     the screen says so rather than sitting empty.
//   - A friend list the player has left public. Steam returns 401 for private
//     ones, which is the player's setting to change, not a bug to work around.

const (
	steamFriendsAPI  = "https://api.steampowered.com/ISteamUser/GetFriendList/v1/"
	friendCacheTTL   = 5 * time.Minute
	friendListLimit  = 200
	friendHTTPBudget = 8 * time.Second
)

// SteamKey is the Web API key. Empty disables the friend list.
var SteamKey string

// friendCache keeps each player's Steam friend list for a few minutes.
//
// Steam rate-limits, the list barely changes, and the menu asks for it every
// time it opens. Caching the *SteamIDs* rather than the resolved profiles is
// deliberate: which of your friends play Kingsreach can change the moment one
// of them signs in, and that is the half worth being fresh.
type friendCache struct {
	mu   sync.Mutex
	byID map[string]friendEntry
	now  func() time.Time
}

type friendEntry struct {
	ids   []string
	until time.Time
	err   error // remembered too: a private list should not be retried every poll
}

func newFriendCache(now func() time.Time) *friendCache {
	return &friendCache{byID: map[string]friendEntry{}, now: now}
}

func (c *friendCache) get(steamID string) ([]string, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byID[steamID]
	if !ok || c.now().After(e.until) {
		return nil, nil, false
	}
	return e.ids, e.err, true
}

func (c *friendCache) put(steamID string, ids []string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID[steamID] = friendEntry{ids: ids, err: err, until: c.now().Add(friendCacheTTL)}
}

// steamFriendIDs asks Steam who this player's friends are.
var steamFriendIDs = func(ctx context.Context, key, steamID string) ([]string, error) {
	q := url.Values{"key": {key}, "steamid": {steamID}, "relationship": {"friend"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamFriendsAPI+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: friendHTTPBudget}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// Steam's answer for "this friend list is private", which is a setting
		// the player owns and we should say so plainly.
		return nil, errPrivateFriends
	default:
		return nil, fmt.Errorf("steam answered %d", res.StatusCode)
	}
	var doc struct {
		FriendsList struct {
			Friends []struct {
				SteamID string `json:"steamid"`
			} `json:"friends"`
		} `json:"friendslist"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.FriendsList.Friends))
	for _, f := range doc.FriendsList.Friends {
		if f.SteamID != "" {
			out = append(out, f.SteamID)
		}
		if len(out) >= friendListLimit {
			break
		}
	}
	return out, nil
}

type privateFriendsError struct{}

func (privateFriendsError) Error() string {
	return "your Steam friend list is private"
}

var errPrivateFriends = privateFriendsError{}

type clientFriend struct {
	ProfileID string `json:"profileId"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar,omitempty"`
	Country   string `json:"country,omitempty"`
	Wins      int    `json:"wins"`
	// Playing is the table they are at right now, if any — the single most
	// useful thing to know about a friend on a game's friend list.
	Playing     string `json:"playing,omitempty"`
	PlayingName string `json:"playingName,omitempty"`
}

// handleFriends lists the caller's Steam friends who have played Kingsreach.
func (s *Server) handleFriends(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProfileByToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	// Guests have no Steam account and therefore no friends to look up. Saying
	// so is more use than an empty list that looks like nobody plays.
	if p.Kind != KindSteam || p.SteamID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"friends": []clientFriend{},
			"reason":  "Friends come from Steam. Sign in through Steam to see yours.",
		})
		return
	}
	if SteamKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"friends": []clientFriend{},
			"reason":  "This server has no Steam API key, so it cannot read friend lists.",
		})
		return
	}

	ids, cachedErr, ok := s.friends.get(p.SteamID)
	if !ok {
		ids, cachedErr = steamFriendIDs(r.Context(), SteamKey, p.SteamID)
		s.friends.put(p.SteamID, ids, cachedErr)
	}
	if cachedErr != nil {
		reason := "Steam would not answer just now; try again in a minute."
		if cachedErr == errPrivateFriends {
			reason = "Your Steam friend list is private, so this can only be as empty as Steam allows."
		} else {
			logf("steam friend list for %s failed: %v", p.SteamID, cachedErr)
		}
		writeJSON(w, http.StatusOK, map[string]any{"friends": []clientFriend{}, "reason": reason})
		return
	}

	profiles, err := s.st.ProfilesBySteamIDs(r.Context(), ids)
	if err != nil {
		logf("friend lookup failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read the friend list")
		return
	}
	out := make([]clientFriend, 0, len(profiles))
	for _, f := range profiles {
		if f.ID == p.ID {
			continue // you are not your own friend, whatever Steam returns
		}
		cf := clientFriend{
			ProfileID: f.ID, Name: f.Name, Avatar: f.Avatar, Country: f.Country, Wins: f.Wins,
		}
		if at, name := s.tableOf(r.Context(), f.ID); at != "" {
			cf.Playing, cf.PlayingName = at, name
		}
		out = append(out, cf)
	}
	sort.Slice(out, func(i, j int) bool {
		// Whoever is at a table right now is the one you might actually join.
		if (out[i].Playing != "") != (out[j].Playing != "") {
			return out[i].Playing != ""
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, http.StatusOK, map[string]any{"friends": out, "total": len(ids)})
}

// tableOf finds the table a profile is sitting at, if any.
func (s *Server) tableOf(ctx context.Context, profileID string) (id, name string) {
	games, err := s.st.ListForProfile(ctx, profileID, 1)
	if err != nil || len(games) == 0 {
		return "", ""
	}
	return games[0].ID, games[0].Name
}
