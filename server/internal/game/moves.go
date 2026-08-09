package game

import "errors"

var (
	ErrGameOver     = errors.New("game is already finished")
	ErrNotYourTurn  = errors.New("not your turn")
	ErrNotPlaying   = errors.New("you are out of this game")
	ErrNoPiece      = errors.New("no piece of yours on that field")
	ErrIllegalMove  = errors.New("illegal move")
	ErrNoCaptures   = errors.New("no strikes in the opening round")
	ErrBadNode      = errors.New("unknown field")
	ErrStillRolling = errors.New("the dice have not decided who opens yet")
	ErrNotRolling   = errors.New("there is nothing to roll for")
	ErrNotYourRoll  = errors.New("not your throw")
)

// legalDests runs a depth-first search over all paths of exactly p.Value
// steps: any direction, turns allowed, no field visited twice within the move
// (including the start), intermediate fields must be empty, and only kings may
// use gold connections or the Throne. The final field may hold a piece the
// mover is allowed to strike (see State.CanTake — never your own, and an
// ally's only under friendly fire) and during the opening round, when captures
// are barred, it must be empty. Returns one sample path per destination.
//
// It takes the state rather than just the occupancy map because "may I take
// that?" stopped being answerable from the two pieces alone once sides existed.
func (b *Board) legalDests(s *State, occ map[NodeID]*Piece, p *Piece, allowCapture bool) map[NodeID][]NodeID {
	res := map[NodeID][]NodeID{}
	isKing := p.Value == 1
	takeable := func(q *Piece) bool { return allowCapture && s.CanTake(p.Owner, q.Owner) }

	var dfs func(cur NodeID, depth int, path []NodeID, visited map[NodeID]bool)
	dfs = func(cur NodeID, depth int, path []NodeID, visited map[NodeID]bool) {
		if depth == p.Value {
			q, taken := occ[cur]
			if !taken || takeable(q) {
				if _, seen := res[cur]; !seen {
					res[cur] = append([]NodeID(nil), path...)
				}
			}
			return
		}
		for _, e := range b.adj[cur] {
			if e.gold && !isKing {
				continue
			}
			if b.Nodes[e.to].Center && !isKing {
				continue
			}
			if visited[e.to] {
				continue
			}
			if q, taken := occ[e.to]; taken {
				// Occupied fields block movement; only the final step may
				// enter one, and only when a strike is on.
				if depth+1 < p.Value || !takeable(q) {
					continue
				}
			}
			visited[e.to] = true
			dfs(e.to, depth+1, append(path, e.to), visited)
			visited[e.to] = false
		}
	}

	visited := map[NodeID]bool{p.Node: true}
	dfs(p.Node, 0, []NodeID{p.Node}, visited)
	return res
}

// LegalMovesFrom returns the legal destinations (with sample paths) for the
// piece standing on from, or an empty map when there is none. Pieces of a
// knocked-out player are frozen — they stay as obstacles but never move —
// unless the table passes a fallen player's stones to their surviving ally.
func (b *Board) LegalMovesFrom(s *State, from NodeID) map[NodeID][]NodeID {
	occ := s.Occupancy()
	p := occ[from]
	if p == nil {
		return map[NodeID][]NodeID{}
	}
	if s.IsOut(p.Owner) {
		if _, ok := s.heir(p.Owner); !ok {
			return map[NodeID][]NodeID{}
		}
	}
	return b.legalDests(s, occ, p, s.CapturesAllowed())
}

// HasAnyLegalMove reports whether seat c has at least one legal move, counting
// any fallen ally's stones they have taken charge of.
func (b *Board) HasAnyLegalMove(s *State, c Color) bool {
	if s.IsOut(c) {
		return false
	}
	occ := s.Occupancy()
	allow := s.CapturesAllowed()
	for _, p := range s.Pieces {
		if p.Captured || !s.MayMove(c, p.Owner) {
			continue
		}
		if len(b.legalDests(s, occ, p, allow)) > 0 {
			return true
		}
	}
	return false
}

// ApplyMove validates and executes mover's move from -> to, mutating s.
// It returns the move record (with the sample path used for animation).
func (b *Board) ApplyMove(s *State, mover Color, from, to NodeID) (*MoveRecord, error) {
	if s.Status != StatusActive {
		return nil, ErrGameOver
	}
	if s.Phase == PhaseRoll {
		return nil, ErrStillRolling
	}
	if s.Turn != mover {
		return nil, ErrNotYourTurn
	}
	if s.IsOut(mover) {
		return nil, ErrNotPlaying
	}
	if !b.HasNode(from) || !b.HasNode(to) {
		return nil, ErrBadNode
	}
	occ := s.Occupancy()
	p := occ[from]
	if p == nil || !s.MayMove(mover, p.Owner) {
		return nil, ErrNoPiece
	}
	allow := s.CapturesAllowed()
	path, ok := b.legalDests(s, occ, p, allow)[to]
	if !ok {
		// Distinguish "that would be a strike, and strikes are not open yet"
		// from an ordinary illegal move, so the client can say why.
		if !allow {
			if q := occ[to]; q != nil && s.CanTake(p.Owner, q.Owner) {
				if _, reachable := b.legalDests(s, occ, p, true)[to]; reachable {
					return nil, ErrNoCaptures
				}
			}
		}
		return nil, ErrIllegalMove
	}

	rec := &MoveRecord{Piece: p.ID, From: from, To: to, Path: path}
	var fallenKing Color
	if q := occ[to]; q != nil {
		q.Captured = true
		q.Node = ""
		rec.Captured = q.ID
		if q.Value == 1 {
			fallenKing = q.Owner
		}
	}
	p.Node = to
	s.Ply++
	s.LastMove = rec

	// Reaching the Throne ends everything at once — only kings can be here.
	if b.Nodes[to].Center {
		s.finish(mover, ReasonThrone)
		return rec, nil
	}
	if fallenKing != "" {
		s.knockOut(fallenKing, OutKingTaken)
		if s.settleIfOneSideLeft() {
			return rec, nil
		}
	}
	if s.Ply >= MaxPlies {
		s.finishDraw(ReasonMoveLimit)
		return rec, nil
	}
	s.advanceTurn(b, mover)
	return rec, nil
}

// advanceTurn hands play to the next seat, knocking out anyone who cannot
// move at all — in Kendo their pieces stay behind as obstacles.
func (s *State) advanceTurn(b *Board, from Color) {
	current := from
	for range s.Seats {
		next, ok := s.nextSeat(current)
		if !ok {
			break
		}
		if b.HasAnyLegalMove(s, next) {
			s.Turn = next
			return
		}
		s.knockOut(next, OutBlocked)
		if s.settleIfOneSideLeft() {
			return
		}
		current = next
	}
	// Nobody can move: everyone still in is stuck.
	if active := s.Active(); len(active) == 1 {
		s.finish(active[0], ReasonLastAlive)
	} else {
		s.finishDraw(ReasonLastAlive)
	}
}
