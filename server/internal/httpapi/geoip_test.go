package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCountryFromProxyHeader(t *testing.T) {
	ts := testServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/geo", nil)
	req.Header.Set("CF-IPCountry", "nl")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if got := decodeCountry(t, res); got != "NL" {
		t.Fatalf("country from CF-IPCountry = %q, want NL", got)
	}
}

// Cloudflare's placeholders are not countries.
func TestUnknownCountryHeadersAreIgnored(t *testing.T) {
	ts := testServer(t)
	for _, code := range []string{"XX", "T1", "", "nonsense"} {
		req, _ := http.NewRequest("GET", ts.URL+"/api/geo", nil)
		req.Header.Set("CF-IPCountry", code)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got := decodeCountry(t, res)
		res.Body.Close()
		if got != "" {
			t.Fatalf("header %q produced country %q, want none", code, got)
		}
	}
}

// A test server is reached over loopback, which no lookup can place — and the
// endpoint must still answer cleanly rather than erroring.
func TestLoopbackYieldsNoCountry(t *testing.T) {
	ts := testServer(t)
	code, data := request(t, "GET", ts.URL+"/api/geo", nil)
	if code != 200 {
		t.Fatalf("geo status = %d", code)
	}
	if data["country"] != "" {
		t.Fatalf("loopback should place nobody, got %v", data["country"])
	}
}

func TestLookupableRejectsPrivateAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.0.0.4", "192.168.1.20", "172.16.3.9", "169.254.1.1", "", "not-an-ip"} {
		if lookupable(ip) {
			t.Fatalf("%s should not be looked up", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !lookupable(ip) {
			t.Fatalf("%s should be looked up", ip)
		}
	}
}

// X-Forwarded-For is attacker-controlled unless we are told to trust it.
func TestForwardedForNeedsTrustProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/geo", nil)
	r.RemoteAddr = "203.0.113.7:5000"
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("without TRUST_PROXY the header must be ignored, got %q", got)
	}

	t.Setenv("TRUST_PROXY", "1")
	if got := clientIP(r); got != "8.8.8.8" {
		t.Fatalf("with TRUST_PROXY the left-most entry wins, got %q", got)
	}
	r.Header.Set("X-Forwarded-For", "8.8.4.4, 10.0.0.1, 10.0.0.2")
	if got := clientIP(r); got != "8.8.4.4" {
		t.Fatalf("chained proxies: got %q, want the original client", got)
	}
}

func decodeCountry(t *testing.T, res *http.Response) string {
	t.Helper()
	var out struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Country
}
