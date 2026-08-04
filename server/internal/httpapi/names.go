package httpapi

import "strings"

// Table names for the computer-hosted tables.
//
// These used to be a dozen tidy English phrases, which read exactly like a
// dozen tidy English phrases written by one person. A board room where every
// table is named in flawless title case is a board room nobody believes in, so
// each host now names their table in their own language, in the register a
// person actually uses — mostly lowercase, often a question, sometimes shouted.
//
// Adding a language: put its code on some hosts and add an entry to
// tableNamesByLang. A language with no entry falls back to English.

type botHost struct {
	Name    string // the handle shown in the room
	Country string
	Lang    string
}

// Where the hosts come from, and the language they name their tables in.
// Repeats are deliberate: they weight how often a country turns up.
var hostOrigins = []struct{ Country, Lang string }{
	{"NL", "nl"}, {"NL", "nl"}, {"BE", "nl"},
	{"DE", "de"}, {"DE", "de"}, {"AT", "de"},
	{"FR", "fr"}, {"FR", "fr"}, {"MA", "fr"},
	{"ES", "es"}, {"ES", "es"}, {"IT", "it"}, {"IT", "it"},
	{"PT", "pt"}, {"BR", "pt"}, {"BR", "pt"},
	{"PL", "pl"}, {"PL", "pl"}, {"CZ", "cs"},
	{"SE", "sv"}, {"DK", "da"}, {"NO", "sv"}, {"FI", "sv"},
	{"TR", "tr"}, {"JP", "ja"}, {"JP", "ja"},
	{"GB", "en"}, {"GB", "en"}, {"US", "en"}, {"US", "en"},
	{"IE", "en"}, {"CA", "en"}, {"AU", "en"}, {"IN", "en"}, {"GH", "en"},
	{"RO", "en"}, {"HU", "en"}, {"GR", "en"}, {"UA", "en"}, {"HR", "en"},
}

// ---- gamertags ----
//
// Handles are built rather than listed: a fixed roster of thirty repeats
// itself within one screen of a busy room. Roughly 45 × 50 parts across nine
// styles is a pool nobody plays through, and the styles are what carry the
// texture — the same word reads completely differently as "FrostRaven",
// "frost_raven99" and "xX_FrostRaven_Xx".

var tagPrefixes = []string{
	"Dark", "Silent", "Mad", "Iron", "Neon", "Toxic", "Frost", "Rapid", "Grim",
	"Lucky", "Ghost", "Turbo", "Cyber", "Nova", "Rusty", "Vex", "Zero", "Hyper",
	"Crimson", "Steel", "Void", "Ash", "Storm", "Blitz", "Rogue", "Feral", "Sly",
	"Wicked", "Pixel", "Retro", "Quantum", "Savage", "Lunar", "Solar", "Cosmic",
	"Phantom", "Venom", "Chaos", "Doom", "Elder", "Wild", "Salty", "Sleepy",
	"Angry", "Tiny",
}

var tagCores = []string{
	"Wolf", "Fox", "Raven", "Hawk", "Bear", "Viper", "Reaper", "Knight", "Rook",
	"Bishop", "Pawn", "King", "Baron", "Duke", "Monk", "Warden", "Ranger",
	"Falcon", "Tiger", "Panda", "Dragon", "Golem", "Wraith", "Blade", "Arrow",
	"Hammer", "Crown", "Throne", "Cipher", "Nomad", "Bandit", "Jester", "Titan",
	"Comet", "Otter", "Badger", "Moose", "Lynx", "Heron", "Goose", "Toast",
	"Waffle", "Pickle", "Noodle", "Muffin", "Squid", "Yeti", "Kraken", "Gremlin",
	"Wizard",
}

// Handles that stand on their own, the way a lot of older accounts do.
var soloTags = []string{
	"Zephyr", "Nyx", "Kobold", "Bones", "Mongoose", "Sputnik", "Basilisk",
	"Halcyon", "Mirage", "Obelisk", "Paprika", "Quokka", "Rhubarb", "Sable",
	"Tundra", "Umbra", "Vandal", "Wombat", "Xerxes", "Yarrow", "Zodiac",
	"Cobblestone", "Marzipan", "Nutmeg", "Oregano", "Pumice", "Quartz",
	"Saffron", "Thistle", "Vellum", "Wicker", "Zinc", "Bramble", "Cinder",
	"Drift", "Ember", "Fathom", "Gale", "Harrow", "Ivory",
}

// Real first names still show up as handles — plenty of people just use theirs
// with a number stuck on. Keeps the room from reading as all teenagers.
var nameTags = []string{
	"marijke", "tomek", "sofia", "anders", "yuki", "ravi", "camille", "diego",
	"lena", "fionn", "mateus", "nadia", "kwame", "elif", "jonas", "aoife",
	"hana", "bram", "ines", "otto", "sven", "emile", "kasia", "mehmet",
	"haruto", "lucia", "sanne", "petr", "freja", "giulia", "dave", "steffi",
	"joris", "mikkel", "pekka", "olek", "rui", "zoltan", "andrei", "niamh",
}

var tagNumbers = []string{
	"42", "99", "77", "01", "07", "13", "21", "88", "69", "1337", "2000", "23",
	"64", "3", "9", "007", "95", "84", "72",
}

// leet maps the substitutions people actually make.
var leet = strings.NewReplacer("o", "0", "e", "3", "i", "1", "a", "4", "s", "5")

// randomHost invents a player: a handle, a country, and the language they will
// name their table in.
func randomHost(pick pickN) botHost {
	origin := hostOrigins[pick(len(hostOrigins))]
	return botHost{Name: gamertag(pick, origin.Country), Country: origin.Country, Lang: origin.Lang}
}

func gamertag(pick pickN, country string) string {
	prefix := tagPrefixes[pick(len(tagPrefixes))]
	core := tagCores[pick(len(tagCores))]
	solo := soloTags[pick(len(soloTags))]
	name := nameTags[pick(len(nameTags))]
	num := tagNumbers[pick(len(tagNumbers))]

	// Weighted, not uniform. xX_..._Xx is unmistakable, so at one style in
	// eleven it stops reading as one player's taste and starts reading as a
	// template; the plainer styles carry the room.
	switch pick(24) {
	case 0, 1, 2:
		return prefix + core // FrostRaven
	case 3, 4, 5:
		return strings.ToLower(prefix) + "_" + strings.ToLower(core) // frost_raven
	case 6, 7, 8:
		return strings.ToLower(prefix+core) + num // frostraven99
	case 9, 10, 11:
		return name + num // marijke92
	case 12, 13:
		return solo + num
	case 14, 15:
		return solo
	case 16, 17:
		return "The" + core // TheRaven
	case 18, 19:
		return name + "_" + strings.ToLower(country) // bram_nl
	case 20, 21:
		return leet.Replace(strings.ToLower(prefix + core)) // fr05tr4v3n
	case 22:
		return "xX_" + prefix + core + "_Xx" // the classic, sparingly
	default:
		return prefix + core + num
	}
}

// nameKey strips the decoration humanise adds, so two tables called
// "tem alguem ai" and "tem alguem ai!!" count as the same name.
func nameKey(s string) string {
	return strings.ToLower(strings.TrimRight(s, "!?. :;)<3"))
}

// Written the way people write them: capitals where someone bothered, none
// where they did not, and a fair share of plain questions.
var tableNamesByLang = map[string][]string{
	"nl": {
		"Avondpotje", "iemand zin in een potje?", "wie durft", "gezellig potje",
		"kom er maar in", "even eentje voor het eten", "ben nieuw, wees aardig",
		"wie wil er spelen", "rustig spelletje", "potje?", "nog 1 dan",
	},
	"de": {
		"Abendrunde", "wer hat lust?", "gemütliche runde", "anfänger sucht gegner",
		"schnelle runde gefällig", "wer traut sich", "komm rein", "Feierabendspiel",
		"noch eine runde", "spielt wer mit?",
	},
	"fr": {
		"Partie du soir", "quelqu'un pour une partie ?", "petite partie tranquille",
		"je débute soyez sympa", "qui veut jouer", "allez on joue", "une partie rapide",
		"qui ose", "encore une",
	},
	"es": {
		"Partida de noche", "alguien para jugar?", "partida tranquila",
		"soy nuevo no seais malos", "quien se anima", "una rapidita", "vamos a jugar",
		"quien se atreve", "otra mas",
	},
	"it": {
		"Partita serale", "qualcuno per una partita?", "partita tranquilla",
		"sono nuovo abbiate pietà", "chi ci sta", "una veloce", "chi osa",
		"giochiamo", "ancora una",
	},
	"pt": {
		"Partida da noite", "alguém pra jogar?", "partida tranquila",
		"sou novo peguem leve", "bora jogar", "uma rapidinha", "quem se atreve",
		"mais uma", "tem alguem ai",
	},
	"pl": {
		"Wieczorna partia", "ktoś chętny?", "spokojna gra", "dopiero się uczę",
		"kto się odważy", "szybka partyjka", "gramy", "jeszcze jedna",
		"ktoś zagra?",
	},
	"sv": {
		"Kvällsparti", "någon som vill spela?", "lugnt parti", "nybörjare här",
		"vem vågar", "en snabb match", "spelar någon", "en till",
	},
	"da": {
		"Aftenparti", "nogen der vil spille?", "roligt spil", "jeg er ny her",
		"hvem tør", "en hurtig en", "spiller nogen", "en mere",
	},
	"cs": {
		"Večerní partie", "někdo na hru?", "klidná hra", "jsem nový buďte hodní",
		"kdo si troufne", "rychlá partie", "hrajeme", "ještě jednu",
	},
	"tr": {
		"Akşam oyunu", "oynayan var mı?", "sakin bir oyun", "yeniyim acımayın",
		"kim var", "hızlı bir oyun", "kim cesaret eder", "bir tane daha",
	},
	"ja": {
		"夜の一局", "誰か一緒にどうですか", "初心者です よろしく", "ゆっくり対局",
		"一局どうぞ", "対戦相手募集", "まだいける", "軽く一局",
	},
	"en": {
		"Evening game", "anyone up for a match?", "casual table, no rush",
		"first time playing, be gentle", "who dares", "quick one before dinner",
		"rematch welcome", "come and lose", "throne rush", "anyone about?",
		"one more before bed",
	},
}

// Decorations people actually add to a lobby name.
var nameTails = []string{"!!", "!!!", "...", " :)", " ;)", " <3"}

// pickN returns a value in [0,n). Callers supply either the crypto source (for
// real tables) or a seeded one (for the drifting "in progress" list).
type pickN func(n int) int

// tableNameFor names a table the way its host would.
func tableNameFor(h botHost, pick pickN) string {
	names := tableNamesByLang[h.Lang]
	if len(names) == 0 {
		names = tableNamesByLang["en"]
	}
	return humanise(names[pick(len(names))], h.Lang, pick)
}

// humanise roughens a name a little: the same phrase typed by two people does
// not come out the same. Deliberately gentle — every roll is a long shot, so
// most names come through untouched and the list never reads as noise.
func humanise(name, lang string, pick pickN) string {
	if pick(5) == 0 { // dropped in a hurry, the way people do
		// French spaces before its question mark, so trim the space as well —
		// otherwise the name ends in whitespace nobody typed.
		name = strings.TrimRight(name, "? ")
	}
	if pick(6) == 0 {
		name += nameTails[pick(len(nameTails))]
	}
	// One shouter per screen is texture; one in twelve is a pattern.
	if pick(20) == 0 && shoutable(lang) {
		name = strings.ToUpper(name)
	}
	return name
}

// shoutable reports whether upper-casing this language is a thing a person
// would do. Japanese has no case at all, and Turkish upper-cases i to İ — a
// naive ToUpper there produces a word no Turkish speaker would write.
func shoutable(lang string) bool {
	switch lang {
	case "ja", "tr":
		return false
	}
	return true
}
