package game

import "math/rand"

// A computer player. It scores every legal move one ply deep and then checks
// what the reply could be, which is enough to make it take free pieces, refuse
// to hang its own king, and walk steadily towards the Throne.

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

// ChooseMove picks a move for seat. Returns ok=false when it has none.
func (b *Board) ChooseMove(s *State, seat Color, rng *rand.Rand) (NodeID, NodeID, bool) {
	if s.Status != StatusActive || s.Phase != PhasePlay || s.IsOut(seat) {
		return "", "", false
	}
	occ := s.Occupancy()
	allow := s.CapturesAllowed()

	best := scoredMove{Score: -1e18}
	found := false
	for _, p := range s.Pieces {
		if p.Captured || p.Owner != seat {
			continue
		}
		for to := range b.legalDests(occ, p, allow) {
			score := b.scoreMove(s, seat, p, to, occ, rng)
			if !found || score > best.Score {
				best = scoredMove{From: p.Node, To: to, Score: score}
				found = true
			}
		}
	}
	if !found {
		return "", "", false
	}
	return best.From, best.To, true
}

func (b *Board) scoreMove(s *State, seat Color, p *Piece, to NodeID, occ map[NodeID]*Piece, rng *rand.Rand) float64 {
	score := 0.0

	// Winning outright beats everything.
	if b.Nodes[to].Center {
		return 1e12
	}
	if victim := occ[to]; victim != nil && victim.Owner != seat {
		if victim.Value == 1 {
			// Taking a king removes a rival for good.
			score += 1e9
		} else {
			score += pieceWorth(victim.Value) * 10
		}
	}

	// Walk the king in, and keep the others from clogging the middle.
	if p.Value == 1 {
		before := b.distanceToThrone(p.Node)
		after := b.distanceToThrone(to)
		score += float64(before-after) * 60
		score += float64(12-after) * 8
	} else {
		after := b.distanceToThrone(to)
		score += float64(12-after) * 3
	}

	// Play the move out and see what it exposes.
	trial := s.Clone()
	if _, err := b.ApplyMove(trial, seat, p.Node, to); err != nil {
		return -1e15 // should not happen; never pick a move we cannot make
	}
	if trial.Status == StatusFinished {
		if trial.Winner == seat {
			return 1e12
		}
		if trial.Winner != "" {
			score -= 1e10 // handing somebody else the win
		}
	}
	// Hanging our own king is close to losing.
	if b.kingInDanger(trial, seat) {
		score -= 5e8
	}
	// Letting a rival king reach the Throne next turn is nearly as bad.
	for _, rival := range trial.Active() {
		if rival == seat {
			continue
		}
		king := trial.King(rival)
		if king == nil || king.Captured {
			continue
		}
		if _, ok := b.LegalMovesFrom(trial, king.Node)[ThroneID]; ok {
			score -= 1e9
		}
	}

	// A nudge of randomness so repeated games do not run identically.
	if rng != nil {
		score += rng.Float64() * 6
	}
	return score
}
