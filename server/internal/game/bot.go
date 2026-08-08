package game

import (
	"math/rand"
	"sort"
)

// A computer player, at three strengths.
//
// The first version scored every legal move one ply deep and took the best.
// It checked whether the move left its own king hanging, but never whether the
// piece it had just moved was now free for the taking — so it walked into every
// trade and played, in a word, badly. `hangs` is the term that fixes that, and
// it is worth more than all the positional tuning put together.
//
// Above that sits an actual search for the hard setting. Two players get plain
// negamax with alpha-beta. Three and four get the "paranoid" simplification —
// assume the single strongest reply from anyone, rather than modelling three
// opponents pursuing their own ends — which is the honest cheap choice for a
// multi-player game and quite strong enough here.

// Difficulty levels. Unknown values behave as Medium.
const (
	Easy   = "easy"
	Medium = "medium"
	Hard   = "hard"
)

// Difficulties is every level, for callers that pick one at random.
var Difficulties = []string{Easy, Medium, Hard}

// winScore is what a won position is worth. Wins are *decayed by distance*:
// without that, "step onto the Throne" and "step next to the Throne and take
// it next turn" score exactly the same, the tie falls to the noise, and the
// bot dawdles in front of an open goal while the opponent gets a free move.
// The step per ply is large enough to beat the randomness and negligible
// against the win itself.
const (
	winScore = 1e12
	plyCost  = 1e6
)

// decay makes a win found sooner worth more than the same win found later,
// and a loss postponed better than a loss now.
func decay(score float64, ply int) float64 {
	switch {
	case score >= winScore:
		return score - float64(ply)*plyCost
	case score <= -winScore:
		return score + float64(ply)*plyCost
	}
	return score
}

// searchDepth is how many plies each level looks ahead.
//
// Depth 1 does not look ahead at all — it scores the position after its own
// move and stops, which is why medium used to walk into replies it could have
// seen coming. Depth 2 is "…and their best answer", which is the least a
// player would call thinking. Hard goes two moves deep for each side.
func searchDepth(difficulty string) int {
	switch difficulty {
	case Hard:
		return 4
	case Easy:
		return 1
	default:
		return 2
	}
}

type scoredMove struct {
	From, To NodeID
	Score    float64
}

// Rough worth of a piece when it is taken.
func pieceWorth(value int) float64 {
	switch value {
	case 1:
		return 900 // the king, but capturing it is scored separately
	case 2:
		return 34
	default:
		return 26
	}
}

// distanceToThrone is the number of steps from a field to the centre, used to
// pull kings inward. The Throne's six corners are one gold step away.
func (b *Board) distanceToThrone(from NodeID) int {
	if from == ThroneID {
		return 0
	}
	seen := map[NodeID]bool{from: true}
	frontier := []NodeID{from}
	for d := 1; d <= 12; d++ {
		next := []NodeID{}
		for _, cur := range frontier {
			for _, e := range b.adj[cur] {
				if seen[e.to] {
					continue
				}
				if e.to == ThroneID {
					return d
				}
				seen[e.to] = true
				next = append(next, e.to)
			}
		}
		if len(next) == 0 {
			break
		}
		frontier = next
	}
	return 12
}

// kingInDanger reports whether any rival can strike seat's king in the position
// as it stands.
func (b *Board) kingInDanger(s *State, seat Color) bool {
	king := s.King(seat)
	if king == nil || king.Captured {
		return false
	}
	occ := s.Occupancy()
	allow := s.CapturesAllowed()
	if !allow {
		return false
	}
	for _, p := range s.Pieces {
		if p.Captured || p.Owner == seat || s.IsOut(p.Owner) {
			continue
		}
		if _, ok := b.legalDests(occ, p, allow)[king.Node]; ok {
			return true
		}
	}
	return false
}

// attackers counts the rival pieces that could take whatever stands on node,
// and defenders counts our own that could take it back.
func (b *Board) tradeAt(s *State, seat Color, node NodeID) (attackers, defenders int) {
	occ := s.Occupancy()
	allow := s.CapturesAllowed()
	if !allow {
		return 0, 0
	}
	for _, p := range s.Pieces {
		if p.Captured || s.IsOut(p.Owner) || p.Node == node {
			continue
		}
		if _, ok := b.legalDests(occ, p, allow)[node]; !ok {
			continue
		}
		if p.Owner == seat {
			defenders++
		} else {
			attackers++
		}
	}
	return attackers, defenders
}

// evaluate scores a whole position from seat's point of view. Positive is good
// for seat. This is what the search maximises, so every idea the bot has about
// the game lives here.
func (b *Board) evaluate(s *State, seat Color) float64 {
	if s.Status == StatusFinished {
		switch {
		case s.Winner == seat:
			return winScore
		case s.Winner == "":
			return 0 // a draw is worth neither win nor loss
		default:
			return -winScore
		}
	}
	if s.IsOut(seat) {
		return -1e11
	}

	score := 0.0
	for _, p := range s.Pieces {
		if p.Captured || s.IsOut(p.Owner) {
			continue
		}
		worth := pieceWorth(p.Value)
		if p.Value == 1 {
			// A king is worth having, and worth having close to the Throne.
			worth += float64(12-b.distanceToThrone(p.Node)) * 22
		} else {
			worth += float64(12-b.distanceToThrone(p.Node)) * 1.5
		}
		if p.Owner == seat {
			score += worth
		} else {
			score -= worth
		}
	}

	// A rival king one step from the Throne is very nearly a loss.
	for _, rival := range s.Active() {
		king := s.King(rival)
		if king == nil || king.Captured {
			continue
		}
		if _, ok := b.LegalMovesFrom(s, king.Node)[ThroneID]; ok {
			if rival == seat {
				score += 4e5
			} else {
				score -= 9e5
			}
		}
	}

	if b.kingInDanger(s, seat) {
		score -= 5e5
	}
	return score
}

// ChooseMove picks a move for seat at the given difficulty. Returns ok=false
// when it has none.
func (b *Board) ChooseMove(s *State, seat Color, difficulty string, rng *rand.Rand) (NodeID, NodeID, bool) {
	if s.Status != StatusActive || s.Phase != PhasePlay || s.IsOut(seat) {
		return "", "", false
	}
	moves := b.rankMoves(s, seat, difficulty, rng)
	if len(moves) == 0 {
		return "", "", false
	}
	pick := moves[0]

	// Easy plays like somebody who has not thought it through: usually one of
	// the better moves rather than the best, and every so often it simply
	// misses a capture that was staring at it.
	if difficulty == Easy && rng != nil {
		if len(moves) > 1 && rng.Intn(100) < 45 {
			pick = moves[1+rng.Intn(min(3, len(moves)-1))]
		}
		if quiet := firstQuietMove(s, moves); quiet != nil && rng.Intn(100) < 25 {
			pick = *quiet
		}
	}
	return pick.From, pick.To, true
}

// firstQuietMove finds a move that takes nothing, so Easy can overlook a
// capture without also throwing the game away.
func firstQuietMove(s *State, moves []scoredMove) *scoredMove {
	occ := s.Occupancy()
	for i := range moves {
		if occ[moves[i].To] == nil {
			return &moves[i]
		}
	}
	return nil
}

// sortedDests is legalDests in a fixed order.
//
// Ranging over the map straight from legalDests is what made the bot
// irreproducible: the random tiebreak in rankMoves draws its numbers in
// iteration order, so the same position from the same seed picked a different
// move on every run. Sorting first costs a few string compares per turn and
// makes a reported game replayable.
func (b *Board) sortedDests(occ map[NodeID]*Piece, p *Piece, allow bool) []NodeID {
	dests := b.legalDests(occ, p, allow)
	out := make([]NodeID, 0, len(dests))
	for to := range dests {
		out = append(out, to)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// rankMoves scores every legal move, best first.
func (b *Board) rankMoves(s *State, seat Color, difficulty string, rng *rand.Rand) []scoredMove {
	occ := s.Occupancy()
	allow := s.CapturesAllowed()
	depth := searchDepth(difficulty)
	noise := 6.0
	if difficulty == Easy {
		noise = 90.0 // enough to shuffle near-equal moves into the wrong order
	}

	out := []scoredMove{}
	for _, p := range s.Pieces {
		if p.Captured || p.Owner != seat {
			continue
		}
		for _, to := range b.sortedDests(occ, p, allow) {
			trial := s.Clone()
			if _, err := b.ApplyMove(trial, seat, p.Node, to); err != nil {
				continue // never offer a move we cannot actually make
			}
			score := decay(b.evaluate(trial, seat), 1) - b.hangs(trial, seat, to)
			if depth > 1 {
				score = b.search(trial, seat, depth-1, 1, -1e18, 1e18)
			}
			if rng != nil {
				score += rng.Float64() * noise
			}
			out = append(out, scoredMove{From: p.Node, To: to, Score: score})
		}
	}
	// Best first. A plain insertion sort is ample for a few dozen moves.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// hangs is the penalty for leaving the piece we just moved where somebody can
// take it. This is the term the first version was missing entirely, and it is
// the difference between a bot that trades sensibly and one that gives its
// army away a piece at a time.
func (b *Board) hangs(after *State, seat Color, at NodeID) float64 {
	piece := after.Occupancy()[at]
	if piece == nil || piece.Owner != seat {
		return 0
	}
	attackers, defenders := b.tradeAt(after, seat, at)
	if attackers == 0 {
		return 0
	}
	loss := pieceWorth(piece.Value)
	if defenders > 0 {
		// It can be recaptured, so we are only down the difference — call it a
		// third of the piece, which is enough to prefer a safe square without
		// making the bot afraid of every exchange.
		loss *= 0.35
	}
	return loss
}

// candidate is one move, ready to be searched.
type candidate struct {
	piece *Piece
	to    NodeID
	grab  float64 // what it takes, for ordering
}

// orderedMoves lists a seat's moves with the captures first.
//
// Alpha-beta only pays for itself when good moves are tried early: a branch
// that is going to be refuted should be refuted on the first reply, not the
// twentieth. Trying captures first is the cheapest ordering that works, and it
// is what makes searching four plies affordable at all.
func (b *Board) orderedMoves(s *State, seat Color, occ map[NodeID]*Piece, allow bool) []candidate {
	out := make([]candidate, 0, 32)
	for _, p := range s.Pieces {
		if p.Captured || p.Owner != seat {
			continue
		}
		for to := range b.legalDests(occ, p, allow) {
			grab := 0.0
			if victim := occ[to]; victim != nil && victim.Owner != seat {
				grab = pieceWorth(victim.Value)
			}
			out = append(out, candidate{piece: p, to: to, grab: grab})
		}
	}
	// Captures first, so alpha-beta has something to cut against — and then a
	// total order on the rest. legalDests hands back a map, so without the
	// tiebreak the equally-scored moves (nearly all of them) come out in Go's
	// randomised map order and the same position played from the same seed
	// gives a different game every run. That costs nothing in strength and
	// buys reproducible bugs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].grab != out[j].grab {
			return out[i].grab > out[j].grab
		}
		if out[i].piece.ID != out[j].piece.ID {
			return out[i].piece.ID < out[j].piece.ID
		}
		return out[i].to < out[j].to
	})
	return out
}

// search is negamax with alpha-beta, run "paranoid" at more than two players:
// every rival is treated as one opponent picking the single reply that hurts
// seat most. Modelling three players each chasing their own win is a different
// and much larger problem than this game needs.
func (b *Board) search(s *State, seat Color, depth, ply int, alpha, beta float64) float64 {
	if depth <= 0 || s.Status == StatusFinished {
		return decay(b.evaluate(s, seat), ply)
	}
	mover := s.Turn
	if s.IsOut(mover) || s.Phase != PhasePlay {
		return decay(b.evaluate(s, seat), ply)
	}
	maximising := mover == seat

	occ := s.Occupancy()
	allow := s.CapturesAllowed()
	best := 1e18
	if maximising {
		best = -1e18
	}
	any := false

	for _, m := range b.orderedMoves(s, mover, occ, allow) {
		{
			p, to := m.piece, m.to
			trial := s.Clone()
			if _, err := b.ApplyMove(trial, mover, p.Node, to); err != nil {
				continue
			}
			any = true
			score := b.search(trial, seat, depth-1, ply+1, alpha, beta)
			if maximising {
				score -= b.hangs(trial, seat, to)
				if score > best {
					best = score
				}
				if best > alpha {
					alpha = best
				}
			} else {
				if score < best {
					best = score
				}
				if best < beta {
					beta = best
				}
			}
			if alpha >= beta {
				return best // this branch is already refuted
			}
		}
	}
	if !any {
		return decay(b.evaluate(s, seat), ply)
	}
	return best
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
