// Package game implements the Kingsreach rules engine: board topology,
// movement validation and win detection. It is pure (no I/O) and fully
// unit-tested; the HTTP layer treats it as the single source of truth.
package game

import (
	"fmt"
	"math"
	"sort"
)

// The board is the vertex graph of a hexagonal patch of 19 flat-top hexagons
// (side 3, axial radius 2), plus one special center node ("the Throne").
//
// Pieces stand on the intersections of the embroidered honeycomb, not inside
// the cells. The Throne is the midpoint of the central hexagon, connected to
// that hexagon's six corners by the six golden connections ("Gilded Paths"),
// which only kings may traverse.
//
// Integer key space: a hex with axial coords (q, r) has center key
// (3q, q+2r); its corners are the center key plus one of the six offsets
// below. Every vertex therefore has a unique integer (kx, ky) key.
// Real render coordinates: x = kx/2, y = ky*sqrt(3)/2 (edge length 1, y up).

// Color identifies a seat. The names are the compass direction of that
// player's home corner, which is also where their camera looks from.
type Color string

const (
	West      Color = "west"
	SouthWest Color = "southwest"
	SouthEast Color = "southeast"
	East      Color = "east"
	NorthEast Color = "northeast"
	NorthWest Color = "northwest"
)

var allSeats = []Color{West, SouthWest, SouthEast, East, NorthEast, NorthWest}

func (c Color) Valid() bool {
	for _, s := range allSeats {
		if s == c {
			return true
		}
	}
	return false
}

type NodeID string

type Node struct {
	ID     NodeID  `json:"id"`
	KX     int     `json:"kx"`
	KY     int     `json:"ky"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Center bool    `json:"center"`
}

type Edge struct {
	A    NodeID `json:"a"`
	B    NodeID `json:"b"`
	Gold bool   `json:"gold"`
}

type adjEntry struct {
	to   NodeID
	gold bool
}

type Board struct {
	Nodes map[NodeID]*Node
	Edges []Edge
	adj   map[NodeID][]adjEntry
}

// BoardHexRadius is the axial radius of the honeycomb patch (2 -> 19 hexes).
const BoardHexRadius = 2

// ThroneID is the id of the goal field in the middle of the board.
const ThroneID NodeID = "0,0"

func keyID(kx, ky int) NodeID { return NodeID(fmt.Sprintf("%d,%d", kx, ky)) }

// Corner offsets in key space, in ring order E, NE, NW, W, SW, SE.
var cornerOffsets = [6][2]int{{2, 0}, {1, 1}, {-1, 1}, {-2, 0}, {-1, -1}, {1, -1}}

func BuildBoard() *Board {
	b := &Board{Nodes: map[NodeID]*Node{}, adj: map[NodeID][]adjEntry{}}

	addNode := func(kx, ky int, center bool) NodeID {
		id := keyID(kx, ky)
		if _, ok := b.Nodes[id]; !ok {
			b.Nodes[id] = &Node{
				ID: id, KX: kx, KY: ky,
				X:      float64(kx) / 2,
				Y:      float64(ky) * math.Sqrt(3) / 2,
				Center: center,
			}
		}
		return id
	}

	edgeSeen := map[string]bool{}
	addEdge := func(a, c NodeID, gold bool) {
		k1, k2 := string(a)+"|"+string(c), string(c)+"|"+string(a)
		if edgeSeen[k1] || edgeSeen[k2] {
			return
		}
		edgeSeen[k1] = true
		b.Edges = append(b.Edges, Edge{A: a, B: c, Gold: gold})
		b.adj[a] = append(b.adj[a], adjEntry{to: c, gold: gold})
		b.adj[c] = append(b.adj[c], adjEntry{to: a, gold: gold})
	}

	for q := -BoardHexRadius; q <= BoardHexRadius; q++ {
		for r := -BoardHexRadius; r <= BoardHexRadius; r++ {
			if maxInt(absInt(q), absInt(r), absInt(q+r)) > BoardHexRadius {
				continue
			}
			cx, cy := 3*q, q+2*r
			var corners [6]NodeID
			for i, o := range cornerOffsets {
				corners[i] = addNode(cx+o[0], cy+o[1], false)
			}
			for i := 0; i < 6; i++ {
				addEdge(corners[i], corners[(i+1)%6], false)
			}
		}
	}

	// The Throne and its six Gilded Paths to the central hexagon's corners.
	throne := addNode(0, 0, true)
	for _, o := range cornerOffsets {
		addEdge(throne, keyID(o[0], o[1]), true)
	}

	sort.Slice(b.Edges, func(i, j int) bool {
		if b.Edges[i].A != b.Edges[j].A {
			return b.Edges[i].A < b.Edges[j].A
		}
		return b.Edges[i].B < b.Edges[j].B
	})
	return b
}

// NodeList returns the nodes in a stable order (for JSON output).
func (b *Board) NodeList() []*Node {
	out := make([]*Node, 0, len(b.Nodes))
	for _, n := range b.Nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].KY != out[j].KY {
			return out[i].KY < out[j].KY
		}
		return out[i].KX < out[j].KX
	})
	return out
}

func (b *Board) HasNode(id NodeID) bool { _, ok := b.Nodes[id]; return ok }

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
