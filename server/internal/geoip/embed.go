package geoip

import (
	_ "embed"
	"log"
	"sync"
)

//go:embed ipv4-country.bin
var packed []byte

var (
	once   sync.Once
	loaded *Table
)

// Builtin returns the table compiled into the binary. It is parsed on first
// use and shared; a corrupt table logs once and disables the lookup rather
// than taking the server down, because a missing flag is not worth an outage.
func Builtin() *Table {
	once.Do(func() {
		t, err := Parse(packed)
		if err != nil {
			log.Printf("geoip: built-in table unusable (%v); country guesses will fall back", err)
			return
		}
		loaded = t
	})
	return loaded
}
