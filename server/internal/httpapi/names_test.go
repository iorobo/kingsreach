package httpapi

import (
	"math/rand"
	"strings"
	"testing"
)

// Every origin must be able to name a table, and in its own language.
func TestEveryHostHasNamesInItsLanguage(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, origin := range hostOrigins {
		h := botHost{Name: "tester", Country: origin.Country, Lang: origin.Lang}
		pool := tableNamesByLang[h.Lang]
		if len(pool) == 0 {
			t.Fatalf("host %s (%s) has language %q with no names", h.Name, h.Country, h.Lang)
		}
		name := tableNameFor(h, rng.Intn)
		if strings.TrimSpace(name) == "" {
			t.Fatalf("host %s produced an empty table name", h.Name)
		}
		// The name must come from that language's pool, decoration aside.
		base := strings.TrimRight(strings.ToLower(name), "!.:;)<3 ")
		found := false
		for _, candidate := range pool {
			if strings.HasPrefix(base, strings.TrimRight(strings.ToLower(candidate), "?")) {
				found = true
			}
		}
		if !found {
			t.Fatalf("host %s (%s) got %q, which is not in the %s pool", h.Name, h.Country, name, h.Lang)
		}
	}
}

// A room where every third table is SHOUTING reads as generated, which is the
// opposite of the point. Decoration must stay rare.
func TestHumanisingStaysRare(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const runs = 4000
	shouted, decorated := 0, 0
	for i := 0; i < runs; i++ {
		h := randomHost(rng.Intn)
		name := tableNameFor(h, rng.Intn)
		if name == strings.ToUpper(name) && strings.ToUpper(name) != strings.ToLower(name) {
			shouted++
		}
		if strings.ContainsAny(name, "!)<") || strings.HasSuffix(name, "...") {
			decorated++
		}
	}
	if rate := float64(shouted) / runs; rate > 0.07 {
		t.Fatalf("%.1f%% of names are shouted, want around 5%%", rate*100)
	}
	if rate := float64(decorated) / runs; rate > 0.30 {
		t.Fatalf("%.1f%% of names carry decoration, want under 30%%", rate*100)
	}
}

// Turkish and Japanese must never be upper-cased: Japanese has no case, and Go
// would turn Turkish "i" into "I" rather than "İ".
func TestNoShoutingWhereItWouldMangle(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 3000; i++ {
		for _, lang := range []string{"tr", "ja"} {
			h := botHost{Name: "X", Country: "XX", Lang: lang}
			name := tableNameFor(h, rng.Intn)
			for _, candidate := range tableNamesByLang[lang] {
				if strings.HasPrefix(name, strings.ToUpper(candidate)) && candidate != strings.ToUpper(candidate) {
					t.Fatalf("%s name was upper-cased: %q", lang, name)
				}
			}
		}
	}
}

// A room full of near-identical handles is worse than a room full of first
// names, so the generator has to actually spread out.
func TestGamertagsAreVaried(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	const runs = 2000
	seen := map[string]bool{}
	for i := 0; i < runs; i++ {
		seen[randomHost(rng.Intn).Name] = true
	}
	if len(seen) < runs*8/10 {
		t.Fatalf("only %d distinct handles in %d draws — the pool is too small", len(seen), runs)
	}
}

// A handle has to survive the display-name trim untouched, or players see a
// truncated stump like "xX_QuantumGremlin_X".
func TestGamertagsFitTheNameField(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for i := 0; i < 4000; i++ {
		h := randomHost(rng.Intn)
		if h.Name == "" {
			t.Fatal("generated an empty handle")
		}
		if cleanName(h.Name) != h.Name {
			t.Fatalf("handle %q (%d runes) does not survive cleanName: %q",
				h.Name, len([]rune(h.Name)), cleanName(h.Name))
		}
		if strings.ContainsAny(h.Name, " \t") {
			t.Fatalf("handle %q contains whitespace", h.Name)
		}
	}
}

// Every host needs a flag, which means a country the client can draw.
func TestEveryOriginHasAFlag(t *testing.T) {
	// The client's flag table is the authority; these are the codes it draws.
	drawable := map[string]bool{}
	for _, c := range []string{
		"AR", "AT", "AU", "BE", "BG", "BR", "CA", "CH", "CL", "CN", "CZ", "DE",
		"DK", "EE", "ES", "FI", "FR", "GB", "GH", "GR", "HR", "HU", "IE", "IN",
		"IS", "IT", "JP", "KR", "LT", "LU", "LV", "MA", "MX", "NL", "NO", "NZ",
		"PL", "PT", "RO", "RS", "SE", "SI", "SK", "TR", "UA", "US", "ZA",
	} {
		drawable[c] = true
	}
	for _, o := range hostOrigins {
		if !drawable[o.Country] {
			t.Fatalf("host country %q has no flag in the client's table", o.Country)
		}
	}
}

// Table names get more room than nicknames — they are short sentences.
func TestTableNamesKeepTheirSentence(t *testing.T) {
	long := "iemand zin in een gezellig potje vanavond?"
	if got := cleanTableName(long); len([]rune(got)) != 40 {
		t.Fatalf("table name trimmed to %d runes, want 40: %q", len([]rune(got)), got)
	}
	if got := cleanName(long); len([]rune(got)) != 24 {
		t.Fatalf("display name should still cap at 24, got %d", len([]rune(got)))
	}
	// Every generated name has to survive the same trim untouched.
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 500; i++ {
		h := randomHost(rng.Intn)
		name := tableNameFor(h, rng.Intn)
		if cleanTableName(name) != name {
			t.Fatalf("generated name does not survive cleaning: %q -> %q", name, cleanTableName(name))
		}
		if name != strings.TrimSpace(name) {
			t.Fatalf("generated name has stray whitespace: %q", name)
		}
	}
}
