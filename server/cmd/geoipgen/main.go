// Command geoipgen turns the geoip2-ipv4 CSV into the compact table that
// internal/geoip embeds.
//
//	go run ./cmd/geoipgen -in geoip2-ipv4.csv -out internal/geoip/ipv4-country.bin
//
// Source: https://github.com/datasets/geoip2-ipv4 (data/geoip2-ipv4.csv),
// refreshed weekly. That dataset is built from MaxMind's GeoLite2 Country
// database, and MaxMind's terms require the credit to be shown — the game
// renders it from client/src/credits.ts. Do not drop it when refreshing.
//
// Rerunning this is the whole update procedure: download the CSV, run the
// command, commit the .bin.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
)

const (
	magic      = "KRGEO1"
	formatVer  = 1
	countryCol = "country_iso_code"
	networkCol = "network"
)

type block struct {
	start, end uint32
	country    uint16
}

func main() {
	in := flag.String("in", "geoip2-ipv4.csv", "path to geoip2-ipv4.csv")
	out := flag.String("out", "internal/geoip/ipv4-country.bin", "table to write")
	flag.Parse()

	blocks, err := readCSV(*in)
	if err != nil {
		log.Fatalf("reading %s: %v", *in, err)
	}
	if len(blocks) == 0 {
		log.Fatal("no usable rows in the CSV")
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].start < blocks[j].start })

	// Boundaries, not ranges. A gap between two blocks becomes an explicit
	// "nobody" marker so a lookup inside it cannot inherit the block before.
	type boundary struct {
		at      uint32
		country uint16
	}
	bounds := make([]boundary, 0, len(blocks)+1)
	var prevEnd uint32
	var haveEnd bool
	for _, b := range blocks {
		if haveEnd && b.start > prevEnd+1 {
			bounds = append(bounds, boundary{at: prevEnd + 1})
		}
		// Runs of the same country collapse into one boundary.
		if n := len(bounds); n > 0 && bounds[n-1].country == b.country && haveEnd && b.start == prevEnd+1 {
			prevEnd = b.end
			continue
		}
		bounds = append(bounds, boundary{at: b.start, country: b.country})
		prevEnd, haveEnd = b.end, true
	}
	if haveEnd && prevEnd != ^uint32(0) {
		bounds = append(bounds, boundary{at: prevEnd + 1})
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatalf("creating %s: %v", *out, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	w.WriteString(magic)
	w.WriteByte(formatVer)
	_ = binary.Write(w, binary.BigEndian, uint32(len(bounds)))
	for _, b := range bounds {
		_ = binary.Write(w, binary.BigEndian, b.at)
		_ = binary.Write(w, binary.BigEndian, b.country)
	}
	if err := w.Flush(); err != nil {
		log.Fatalf("writing %s: %v", *out, err)
	}
	fi, _ := f.Stat()
	fmt.Printf("%d blocks -> %d boundaries, %.1f MB written to %s\n",
		len(blocks), len(bounds), float64(fi.Size())/(1<<20), *out)
}

func readCSV(path string) ([]block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	r.ReuseRecord = true
	head, err := r.Read()
	if err != nil {
		return nil, err
	}
	netAt, ccAt := -1, -1
	for i, name := range head {
		switch strings.TrimSpace(name) {
		case networkCol:
			netAt = i
		case countryCol:
			ccAt = i
		}
	}
	if netAt < 0 || ccAt < 0 {
		return nil, fmt.Errorf("CSV is missing %q or %q", networkCol, countryCol)
	}

	out := make([]block, 0, 600_000)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		cc := strings.ToUpper(strings.TrimSpace(rec[ccAt]))
		if len(cc) != 2 { // some blocks are registered to no country at all
			continue
		}
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(rec[netAt]))
		if err != nil || ipnet.IP.To4() == nil {
			continue
		}
		start := binary.BigEndian.Uint32(ipnet.IP.To4())
		mask := binary.BigEndian.Uint32(net.IP(ipnet.Mask).To4())
		out = append(out, block{
			start:   start,
			end:     start | ^mask,
			country: uint16(cc[0])<<8 | uint16(cc[1]),
		})
	}
	return out, nil
}
