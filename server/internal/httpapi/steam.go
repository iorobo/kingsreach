package httpapi

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Steam sign-in over OpenID 2.0. Steam is the only party that can confirm an
// identity, so the callback is never trusted on its own: every return trip is
// posted straight back to Steam for verification before a profile is touched.

const (
	steamOpenIDEndpoint = "https://steamcommunity.com/openid/login"
	steamIdentifier     = "http://specs.openid.net/auth/2.0/identifier_select"
)

var steamIDPattern = regexp.MustCompile(`^https?://steamcommunity\.com/openid/id/(\d{17})$`)

// SteamAuth performs the OpenID dance. Both outbound calls are fields so tests
// can run the whole flow without touching the network.
type SteamAuth struct {
	// Realm is the site Steam sends the player back to, e.g. http://localhost:8080
	Realm string
	// Verify asks Steam whether a callback is genuine.
	Verify func(ctx context.Context, params url.Values) (bool, error)
	// Summary fetches the display name and avatar for a verified SteamID.
	Summary func(ctx context.Context, steamID string) (name, avatar string, err error)
}

func NewSteamAuth(realm string) *SteamAuth {
	client := &http.Client{Timeout: 8 * time.Second}
	return &SteamAuth{
		Realm:   strings.TrimRight(realm, "/"),
		Verify:  func(ctx context.Context, p url.Values) (bool, error) { return verifyWithSteam(ctx, client, p) },
		Summary: func(ctx context.Context, id string) (string, string, error) { return steamSummary(ctx, client, id) },
	}
}

// LoginURL is where the player is sent to sign in.
func (s *SteamAuth) LoginURL(returnTo string) string {
	q := url.Values{
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {s.Realm},
		"openid.identity":   {steamIdentifier},
		"openid.claimed_id": {steamIdentifier},
	}
	return steamOpenIDEndpoint + "?" + q.Encode()
}

// SteamIDFromCallback verifies the callback with Steam and returns the 64-bit
// SteamID it vouches for.
func (s *SteamAuth) SteamIDFromCallback(ctx context.Context, q url.Values) (string, error) {
	claimed := q.Get("openid.claimed_id")
	m := steamIDPattern.FindStringSubmatch(claimed)
	if m == nil {
		return "", errors.New("that sign-in did not come from Steam")
	}
	ok, err := s.Verify(ctx, q)
	if err != nil {
		return "", fmt.Errorf("could not reach Steam: %w", err)
	}
	if !ok {
		return "", errors.New("Steam did not confirm that sign-in")
	}
	return m[1], nil
}

// verifyWithSteam replays the callback to Steam with mode=check_authentication,
// which is the only way to know the parameters were not forged.
func verifyWithSteam(ctx context.Context, client *http.Client, params url.Values) (bool, error) {
	form := url.Values{}
	for k, v := range params {
		if strings.HasPrefix(k, "openid.") && len(v) > 0 {
			form.Set(k, v[0])
		}
	}
	form.Set("openid.mode", "check_authentication")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, steamOpenIDEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "is_valid:true" {
			return true, nil
		}
	}
	return false, nil
}

// steamSummary reads the public profile XML — no API key needed, unlike the
// Web API — for the display name and avatar.
func steamSummary(ctx context.Context, client *http.Client, steamID string) (string, string, error) {
	u := fmt.Sprintf("https://steamcommunity.com/profiles/%s/?xml=1", steamID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	var doc struct {
		SteamID    string `xml:"steamID"`
		AvatarFull string `xml:"avatarFull"`
		AvatarIcon string `xml:"avatarMedium"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 256*1024)).Decode(&doc); err != nil {
		return "", "", err
	}
	avatar := doc.AvatarIcon
	if avatar == "" {
		avatar = doc.AvatarFull
	}
	return strings.TrimSpace(doc.SteamID), strings.TrimSpace(avatar), nil
}
