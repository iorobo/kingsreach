package geoip

import "testing"

// Spot checks. The expected values are what the source CSV assigns, not what
// the operator of the address is headquartered as — 1.1.1.1 is Cloudflare's
// resolver but the block is registered to APNIC in Australia, and a geo
// database answers about registration.
func TestBuiltinTableLocatesKnownAddresses(t *testing.T) {
	tbl := Builtin()
	if tbl == nil {
		t.Fatal("the built-in table failed to load")
	}
	if tbl.Len() < 100_000 {
		t.Fatalf("table has only %d boundaries — did the generator run on a truncated CSV?", tbl.Len())
	}
	for ip, want := range map[string]string{
		"8.8.8.8":         "US", // 8.8.8.0/24, Google
		"1.1.1.1":         "AU", // 1.1.1.0/24, APNIC
		"1.1.0.1":         "CN", // the block next door, so the boundary is exact
		"145.53.1.1":      "NL",
		"213.46.1.1":      "NL",
		"195.130.130.130": "BE", // inside 195.130.128.0/19
		"203.0.113.1":     "",   // TEST-NET-3, allocated to nobody
	} {
		if got := tbl.Lookup(ip); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", ip, got, want)
		}
	}
}

// Everything that is not an IPv4 address must come back empty, not wrong.
func TestLookupRejectsWhatItCannotAnswer(t *testing.T) {
	tbl := Builtin()
	for _, ip := range []string{
		"",
		"not-an-ip",
		"999.1.1.1",
		"2606:4700:4700::1111", // IPv6 is outside this dataset
		"::1",
	} {
		if got := tbl.Lookup(ip); got != "" {
			t.Errorf("Lookup(%q) = %q, want empty", ip, got)
		}
	}
	var nilTable *Table
	if got := nilTable.Lookup("8.8.8.8"); got != "" {
		t.Errorf("a nil table must answer empty, got %q", got)
	}
}

// The boundaries have to be sorted, or the binary search silently lies.
func TestBoundariesAreSorted(t *testing.T) {
	tbl := Builtin()
	for i := 1; i < len(tbl.starts); i++ {
		if tbl.starts[i] <= tbl.starts[i-1] {
			t.Fatalf("boundary %d (%d) does not follow %d", i, tbl.starts[i], tbl.starts[i-1])
		}
	}
}

func TestParseRejectsRubbish(t *testing.T) {
	for _, raw := range [][]byte{
		nil,
		[]byte("nope"),
		append([]byte(magic), 99, 0, 0, 0, 1), // wrong version
		append([]byte(magic), formatVer, 0, 0, 0, 5), // count without a body
	} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("Parse(%q) should have failed", raw)
		}
	}
}

func TestCountryCodesRoundTrip(t *testing.T) {
	for _, cc := range []string{"NL", "US", "JP", "ZA"} {
		if got := decode(encode(cc)); got != cc {
			t.Fatalf("%s round-tripped to %q", cc, got)
		}
	}
	if decode(encode("")) != "" || decode(encode("XYZ")) != "" {
		t.Fatal("an invalid code must encode to unknown")
	}
}

func BenchmarkLookup(b *testing.B) {
	tbl := Builtin()
	for i := 0; i < b.N; i++ {
		tbl.Lookup("145.53.1.1")
	}
}
