package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Guessing a player's country from their address, to preselect the flag in the
// sign-in picker. It is a *suggestion*: the player sees it in the dropdown and
// can change it before it ever reaches their profile.
//
// Two sources, in order:
//
//  1. A country header from whatever sits in front of us. Cloudflare, Fastly
//     and friends already know, so this costs nothing and leaks nothing.
//  2. A lookup service, if one is configured. This necessarily hands the
//     player's IP to a third party, so it is opt-in through GEOIP_URL and the
//     result is cached per address.
//
// Private, loopback and unparseable addresses are never looked up.

// Headers a reverse proxy or CDN may set. All carry a bare alpha-2 code.
var countryHeaders = []string{
	"CF-IPCountry", // Cloudflare
	"CloudFront-Viewer-Country",
	"X-Vercel-IP-Country",
	"Fastly-Client-Country",
	"X-Country-Code",
	"X-Geo-Country",
}

type geoCache struct {
	mu      sync.Mutex
	entries map[string]geoEntry
}

type geoEntry struct {
	country string
	at      time.Time
}

const geoTTL = 12 * time.Hour

func newGeoCache() *geoCache {
	return &geoCache{entries: map[string]geoEntry{}}
}

func (c *geoCache) get(ip string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[ip]
	if !ok || time.Since(e.at) > geoTTL {
		return "", false
	}
	return e.country, true
}

func (c *geoCache) put(ip, country string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The cache is a courtesy, not a store: drop it wholesale rather than
	// growing without bound or holding addresses longer than the TTL.
	if len(c.entries) > 5000 {
		c.entries = map[string]geoEntry{}
	}
	c.entries[ip] = geoEntry{country: country, at: time.Now()}
}

// clientIP is the address we believe the request came from. X-Forwarded-For is
// only trusted when TRUST_PROXY is set, because anyone can send that header.
func clientIP(r *http.Request) string {
	if truthyEnv("TRUST_PROXY") {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			// Left-most entry is the original client.
			if first, _, ok := strings.Cut(fwd, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(fwd)
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// lookupable rejects the addresses no service can say anything useful about.
func lookupable(ip string) bool {
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	return !addr.IsLoopback() && !addr.IsPrivate() && !addr.IsUnspecified() &&
		!addr.IsLinkLocalUnicast() && !addr.IsLinkLocalMulticast()
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// handleGeo answers with the country we would guess, or an empty string when
// we have nothing to go on. Never an error: a failed guess is not a failure.
func (s *Server) handleGeo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"country": s.guessCountry(r)})
}

func (s *Server) guessCountry(r *http.Request) string {
	for _, h := range countryHeaders {
		// Cloudflare sends XX for "unknown" and T1 for Tor.
		if c := cleanCountry(r.Header.Get(h)); c != "" && c != "XX" && c != "T1" {
			return c
		}
	}
	ip := clientIP(r)
	if !lookupable(ip) {
		return ""
	}
	if c, ok := s.geo.get(ip); ok {
		return c
	}
	country := s.lookupCountry(r.Context(), ip)
	s.geo.put(ip, country) // cache misses too, so a dead service is asked once
	return country
}

// lookupCountry asks the configured service. GEOIP_URL must contain "{ip}";
// the reply is read as JSON with a country_code/countryCode field, or as a
// bare two-letter body.
func (s *Server) lookupCountry(ctx context.Context, ip string) string {
	tmpl := os.Getenv("GEOIP_URL")
	if tmpl == "" || !strings.Contains(tmpl, "{ip}") {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.ReplaceAll(tmpl, "{ip}", ip), nil)
	if err != nil {
		return ""
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		logf("geoip lookup failed: %v", err)
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return ""
	}

	var payload struct {
		CountryCode  string `json:"country_code"`
		CountryCode2 string `json:"countryCode"`
		Country      string `json:"country"`
	}
	if json.Unmarshal(body, &payload) == nil {
		for _, c := range []string{payload.CountryCode, payload.CountryCode2, payload.Country} {
			if got := cleanCountry(c); got != "" {
				return got
			}
		}
	}
	return cleanCountry(string(body))
}
