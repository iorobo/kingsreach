// Package geoip answers "which country is this IPv4 address in?" from a table
// compiled into the binary, without asking anybody.
//
// The alternative — calling a lookup service — hands every player's address to
// a third party, needs the network to be up, and comes with rate limits. This
// costs a few megabytes of binary and answers in a couple of hundred
// nanoseconds.
//
// The table is a sorted list of boundaries rather than a list of ranges: each
// entry says "from this address onward, this country", and an entry with no
// country marks unallocated space. That halves the size, because the end of
// one block is the start of the next, and a binary search over boundaries is
// the same search either way.
//
// IPv4 only, which is the dataset's limit and worth knowing: an IPv6 caller
// gets no answer here and has to fall back to something else.
package geoip

import (
	"encoding/binary"
	"errors"
	"net"
	"sort"
)

// Format: magic, version, count, then count × (start uint32, country uint16),
// all big-endian and sorted by start.
const (
	magic       = "KRGEO1"
	entrySize   = 6
	headerSize  = len(magic) + 1 + 4
	formatVer   = 1
	unknownCode = 0
)

var ErrBadTable = errors.New("geoip: table is not in the expected format")

type Table struct {
	starts    []uint32
	countries []uint16
}

// Parse reads a table produced by the generator in cmd/geoipgen.
func Parse(raw []byte) (*Table, error) {
	if len(raw) < headerSize || string(raw[:len(magic)]) != magic {
		return nil, ErrBadTable
	}
	if raw[len(magic)] != formatVer {
		return nil, ErrBadTable
	}
	count := int(binary.BigEndian.Uint32(raw[len(magic)+1:]))
	body := raw[headerSize:]
	if len(body) != count*entrySize {
		return nil, ErrBadTable
	}
	t := &Table{starts: make([]uint32, count), countries: make([]uint16, count)}
	for i := 0; i < count; i++ {
		off := i * entrySize
		t.starts[i] = binary.BigEndian.Uint32(body[off:])
		t.countries[i] = binary.BigEndian.Uint16(body[off+4:])
	}
	return t, nil
}

// Len is the number of boundaries in the table.
func (t *Table) Len() int { return len(t.starts) }

// Lookup returns the ISO-3166 alpha-2 code for an address, or "" when the
// address is IPv6, unparseable, or falls in space the table does not cover.
func (t *Table) Lookup(ip string) string {
	if t == nil {
		return ""
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return ""
	}
	v4 := addr.To4()
	if v4 == nil {
		return "" // IPv6: not in this dataset
	}
	n := binary.BigEndian.Uint32(v4)

	// The last boundary at or below the address owns it.
	i := sort.Search(len(t.starts), func(i int) bool { return t.starts[i] > n })
	if i == 0 {
		return ""
	}
	return decode(t.countries[i-1])
}

// Countries are two ASCII letters packed into a uint16, so the table needs no
// string storage at all.
func encode(cc string) uint16 {
	if len(cc) != 2 {
		return unknownCode
	}
	return uint16(cc[0])<<8 | uint16(cc[1])
}

func decode(v uint16) string {
	if v == unknownCode {
		return ""
	}
	return string([]byte{byte(v >> 8), byte(v)})
}
