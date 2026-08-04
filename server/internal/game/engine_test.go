package game

import "testing"

func board(t *testing.T) *Board {
	t.Helper()
	return BuildBoard()
}

// craft builds a custom position, past the dice and past the opening round so
// captures are live; tests that care about either set those fields themselves.
func craft(seats []Color, turn Color, pieces ...*Piece) *State {
	return &State{
		Seats: seats, Pieces: pieces, Turn: turn,
		Status: StatusActive, Phase: PhasePlay, Ply: len(seats),
	}
}

// started returns a fresh game with the dice already settled on `first`.
func started(seats []Color, first Color) *State {
	s := NewState(seats, first)
	s.Phase = PhasePlay
	s.Pending = nil
	s.Turn = first
	return s
}

func duel(turn Color, pieces ...*Piece) *State {
	return craft([]Color{West, East}, turn, pieces...)
}

func pc(id PieceID, owner Color, value int, node NodeID) *Piece {
	return &Piece{ID: id, Owner: owner, Value: value, Node: node}
}

func TestBoardCounts(t *testing.T) {
	b := board(t)
	if got := len(b.Nodes); got != 55 {
		t.Fatalf("nodes = %d, want 55 (54 vertices + Throne)", got)
	}
	if got := len(b.Edges); got != 78 {
		t.Fatalf("edges = %d, want 78 (72 teal + 6 gold)", got)
	}
	gold := 0
	for _, e := range b.Edges {
		if e.Gold {
			gold++
		}
	}
	if gold != 6 {
		t.Fatalf("gold edges = %d, want 6", gold)
	}
	if got := len(b.adj[ThroneID]); got != 6 {
		t.Fatalf("throne degree = %d, want 6", got)
	}
}

// The board has six-fold symmetry, which is what lets every seat reuse the
// west formation rotated about the centre.
func TestBoardRotationalSymmetry(t *testing.T) {
	b := board(t)
	for id, n := range b.Nodes {
		if n.Center {
			continue
		}
		kx, ky := n.KX, n.KY
		for turn := 1; turn <= 6; turn++ {
			kx, ky = rotate60(kx, ky)
			if !b.HasNode(keyID(kx, ky)) {
				t.Fatalf("rotating %s by %d*60° leaves the board at %d,%d", id, turn, kx, ky)
			}
		}
		if kx != n.KX || ky != n.KY {
			t.Fatalf("six rotations of %s should return home, got %d,%d", id, kx, ky)
		}
	}
}

func TestInitialSetupTwoPlayers(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	if len(s.Pieces) != 16 {
		t.Fatalf("pieces = %d, want 16", len(s.Pieces))
	}
	if len(s.Occupancy()) != 16 {
		t.Fatalf("distinct occupied nodes = %d, want 16 (no overlaps)", len(s.Occupancy()))
	}
	for _, p := range s.Pieces {
		if !b.HasNode(p.Node) {
			t.Fatalf("piece %s on nonexistent node %s", p.ID, p.Node)
		}
	}
	if s.King(West).Node != "-8,0" || s.King(East).Node != "8,0" {
		t.Fatalf("kings misplaced: %s / %s", s.King(West).Node, s.King(East).Node)
	}
	counts := map[Color]map[int]int{West: {}, East: {}}
	for _, p := range s.Pieces {
		counts[p.Owner][p.Value]++
	}
	for _, c := range []Color{West, East} {
		if counts[c][1] != 1 || counts[c][2] != 3 || counts[c][3] != 4 {
			t.Fatalf("%s composition = %v, want 1 king, 3 twos, 4 threes", c, counts[c])
		}
	}
}

func TestInitialSetupFourPlayers(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	if len(seats) != 4 {
		t.Fatalf("four-player seats = %v", seats)
	}
	s := started(seats, seats[0])
	if len(s.Pieces) != 32 {
		t.Fatalf("pieces = %d, want 32", len(s.Pieces))
	}
	if len(s.Occupancy()) != 32 {
		t.Fatalf("occupied nodes = %d, want 32 — seats must not overlap", len(s.Occupancy()))
	}
	for _, p := range s.Pieces {
		if !b.HasNode(p.Node) {
			t.Fatalf("piece %s on nonexistent node %s", p.ID, p.Node)
		}
	}
	for _, seat := range seats {
		counts := map[int]int{}
		for _, p := range s.Pieces {
			if p.Owner == seat {
				counts[p.Value]++
			}
		}
		if counts[1] != 1 || counts[2] != 3 || counts[3] != 4 {
			t.Fatalf("%s composition = %v, want 1/3/4", seat, counts)
		}
		if b.HasAnyLegalMove(s, seat) == false {
			t.Fatalf("%s has no legal move at setup", seat)
		}
	}
	// Opposite pairs: west/east and southwest/northeast face each other.
	if s.King(West).Node != "-8,0" || s.King(East).Node != "8,0" {
		t.Fatalf("west/east kings misplaced")
	}
	if s.King(SouthWest).Node != "-4,-4" || s.King(NorthEast).Node != "4,4" {
		t.Fatalf("southwest/northeast kings misplaced: %s / %s",
			s.King(SouthWest).Node, s.King(NorthEast).Node)
	}
}

func TestTurnOrderRunsRoundTheTable(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	s := started(seats, seats[0])
	seen := []Color{s.Turn}
	for i := 0; i < 4; i++ {
		from := s.Turn
		moved := false
		for _, p := range s.Pieces {
			if p.Captured || p.Owner != from {
				continue
			}
			for to := range b.LegalMovesFrom(s, p.Node) {
				if _, err := b.ApplyMove(s, from, p.Node, to); err == nil {
					moved = true
				}
				break
			}
			if moved {
				break
			}
		}
		if !moved {
			t.Fatalf("%s could not move", from)
		}
		seen = append(seen, s.Turn)
	}
	for i, seat := range seats {
		if seen[i] != seat {
			t.Fatalf("turn order = %v, want %v", seen[:len(seats)], seats)
		}
	}
	if seen[4] != seats[0] {
		t.Fatalf("turn should wrap to %s, got %s", seats[0], seen[4])
	}
}

func TestExactStepsAndNoBounce(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	dests := b.LegalMovesFrom(s, "-4,0")
	if len(dests) != 2 {
		t.Fatalf("2 on -4,0: got %d dests %v, want exactly 2", len(dests), keys(dests))
	}
	for _, want := range []NodeID{"-1,1", "-1,-1"} {
		if _, ok := dests[want]; !ok {
			t.Fatalf("2 on -4,0: missing dest %s (got %v)", want, keys(dests))
		}
	}
	if _, ok := dests["-4,0"]; ok {
		t.Fatalf("a 2 may never bounce back to its start field")
	}
	for _, p := range dests {
		if len(p) != 3 {
			t.Fatalf("2-move path length = %d nodes, want 3", len(p))
		}
	}
}

func TestBlockedPassThrough(t *testing.T) {
	b := board(t)
	s := duel(West,
		pc("wK", West, 1, "-8,0"), pc("eK", East, 1, "8,0"),
		pc("w2a", West, 2, "-4,0"), pc("e3a", East, 3, "-2,0"),
	)
	dests := b.LegalMovesFrom(s, "-4,0")
	for _, forbidden := range []NodeID{"-1,1", "-1,-1"} {
		if _, ok := dests[forbidden]; ok {
			t.Fatalf("dest %s should be unreachable (blocked by piece on -2,0), got %v", forbidden, keys(dests))
		}
	}
	if len(dests) == 0 {
		t.Fatalf("2 on -4,0 should still have moves around the west side")
	}
}

func TestCapture(t *testing.T) {
	b := board(t)
	s := duel(West,
		pc("wK", West, 1, "-8,0"), pc("eK", East, 1, "8,0"),
		pc("w2a", West, 2, "-4,0"), pc("e3a", East, 3, "-1,1"),
	)
	rec, err := b.ApplyMove(s, West, "-4,0", "-1,1")
	if err != nil {
		t.Fatalf("capture move rejected: %v", err)
	}
	if rec.Captured != "e3a" {
		t.Fatalf("captured = %q, want e3a", rec.Captured)
	}
	e3 := s.PieceByID("e3a")
	if !e3.Captured || e3.Node != "" {
		t.Fatalf("captured piece not removed: %+v", e3)
	}
	if s.Status != StatusActive || s.Turn != East {
		t.Fatalf("game should continue with east to move, got %s/%s", s.Status, s.Turn)
	}
	if s.Occupancy()["-1,1"].ID != "w2a" {
		t.Fatalf("attacker should occupy the struck field")
	}
}

// Kendo protects the opening: nobody may be struck until every seat has moved.
func TestNoCapturesInOpeningRound(t *testing.T) {
	b := board(t)
	s := duel(West,
		pc("wK", West, 1, "-8,0"), pc("eK", East, 1, "8,0"),
		pc("w2a", West, 2, "-4,0"), pc("e3a", East, 3, "-1,1"),
	)
	s.Ply = 0 // opening round
	if s.CapturesAllowed() {
		t.Fatalf("captures should be barred at ply 0 of a 2-player game")
	}
	if _, ok := b.LegalMovesFrom(s, "-4,0")["-1,1"]; ok {
		t.Fatalf("the struck field must not be offered during the opening round")
	}
	if _, err := b.ApplyMove(s, West, "-4,0", "-1,1"); err != ErrNoCaptures {
		t.Fatalf("opening strike: err = %v, want ErrNoCaptures", err)
	}

	// One move each and the protection lifts.
	if _, err := b.ApplyMove(s, West, "-4,0", "-1,-1"); err != nil {
		t.Fatalf("quiet opening move rejected: %v", err)
	}
	if _, err := b.ApplyMove(s, East, "8,0", "7,1"); err != nil {
		t.Fatalf("east opening move rejected: %v", err)
	}
	if !s.CapturesAllowed() {
		t.Fatalf("captures should be live once both seats have moved")
	}
}

func TestGoldPathsKingOnly(t *testing.T) {
	b := board(t)
	s := duel(West, pc("wK", West, 1, "-8,0"), pc("eK", East, 1, "8,0"), pc("w2a", West, 2, "2,0"))
	if _, ok := b.LegalMovesFrom(s, "2,0")[ThroneID]; ok {
		t.Fatalf("non-king reached the Throne")
	}
	s2 := duel(West, pc("wK", West, 1, "2,0"), pc("eK", East, 1, "8,0"))
	if _, ok := b.LegalMovesFrom(s2, "2,0")[ThroneID]; !ok {
		t.Fatalf("king should be able to enter the Throne from a central corner")
	}
	if _, err := b.ApplyMove(s2, West, "2,0", ThroneID); err != nil {
		t.Fatalf("king->Throne rejected: %v", err)
	}
	if s2.Status != StatusFinished || s2.Winner != West || s2.WinReason != ReasonThrone {
		t.Fatalf("throne win not detected: %+v", s2)
	}
}

func TestKingCaptureEndsTwoPlayerGame(t *testing.T) {
	b := board(t)
	s := duel(East, pc("wK", West, 1, "-8,0"), pc("eK", East, 1, "8,0"), pc("e2a", East, 2, "-5,1"))
	rec, err := b.ApplyMove(s, East, "-5,1", "-8,0")
	if err != nil {
		t.Fatalf("king capture rejected: %v", err)
	}
	if rec.Captured != "wK" {
		t.Fatalf("captured = %q, want wK", rec.Captured)
	}
	if !s.IsOut(West) {
		t.Fatalf("west should be knocked out")
	}
	if s.Status != StatusFinished || s.Winner != East || s.WinReason != ReasonLastAlive {
		t.Fatalf("last player standing should win: %+v", s)
	}
}

// With four players, losing your king does not end the game — you drop out and
// your pieces stay put as obstacles.
func TestKingCaptureKnocksOutButGameGoesOn(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	s := craft(seats, East,
		pc("wK", West, 1, "-8,0"), pc("w3a", West, 3, "-8,2"),
		pc("eK", East, 1, "8,0"), pc("e2a", East, 2, "-5,1"),
		pc("sK", SouthWest, 1, "-4,-4"),
		pc("nK", NorthEast, 1, "4,4"),
	)
	if _, err := b.ApplyMove(s, East, "-5,1", "-8,0"); err != nil {
		t.Fatalf("king capture rejected: %v", err)
	}
	if !s.IsOut(West) {
		t.Fatalf("west should be out after losing its king")
	}
	if s.Status != StatusActive {
		t.Fatalf("game should continue with three players left, got %s (%s)", s.Status, s.WinReason)
	}
	// The fallen player's other piece stays on the board and still blocks.
	stray := s.PieceByID("w3a")
	if stray.Captured || stray.Node != "-8,2" {
		t.Fatalf("knocked-out player's pieces must stay on the board: %+v", stray)
	}
	if len(b.LegalMovesFrom(s, "-8,2")) != 0 {
		t.Fatalf("a knocked-out player's pieces must not be movable")
	}
	// Turn passes to the next living seat in board order (east -> northeast).
	if s.Turn != NorthEast {
		t.Fatalf("turn = %s, want northeast", s.Turn)
	}
}

func TestStalemateKnocksPlayerOut(t *testing.T) {
	b := board(t)
	s := duel(East,
		pc("wK", West, 1, "-8,0"),
		pc("w3a", West, 3, "-7,1"), pc("w3b", West, 3, "-7,-1"),
		pc("e3a", East, 3, "-5,1"), pc("e3b", East, 3, "-5,-1"),
		pc("e3c", East, 3, "-8,-2"), pc("e3d", East, 3, "-4,2"),
		pc("eK", East, 1, "8,0"),
	)
	if !b.HasAnyLegalMove(s, West) {
		t.Fatalf("west should still have a move before the block")
	}
	if _, err := b.ApplyMove(s, East, "-4,2", "-8,2"); err != nil {
		t.Fatalf("blocking move rejected: %v", err)
	}
	if !s.IsOut(West) {
		t.Fatalf("a player with no legal move drops out")
	}
	if s.Status != StatusFinished || s.Winner != East || s.WinReason != ReasonLastAlive {
		t.Fatalf("sole survivor should win: %+v", s)
	}
}

func TestTurnAndOwnershipEnforced(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	if _, err := b.ApplyMove(s, East, "5,-1", "2,0"); err != ErrNotYourTurn {
		t.Fatalf("moving out of turn: err = %v, want ErrNotYourTurn", err)
	}
	if _, err := b.ApplyMove(s, West, "5,-1", "2,0"); err != ErrNoPiece {
		t.Fatalf("moving a rival's piece: err = %v, want ErrNoPiece", err)
	}
	if _, err := b.ApplyMove(s, West, "-4,0", "-4,0"); err != ErrIllegalMove {
		t.Fatalf("null move: err = %v, want ErrIllegalMove", err)
	}
}

func TestResignKnocksOut(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	if err := s.Resign(b, West); err != nil {
		t.Fatalf("resign failed: %v", err)
	}
	if !s.IsOut(West) || s.Status != StatusFinished || s.Winner != East {
		t.Fatalf("resign outcome wrong: %+v", s)
	}

	four := started(SeatOrder(4), West)
	if err := four.Resign(b, West); err != nil {
		t.Fatalf("resign failed: %v", err)
	}
	if four.Status != StatusActive {
		t.Fatalf("one resignation should not end a four-player game: %s", four.Status)
	}
	if four.Turn == West {
		t.Fatalf("turn should have moved on from the resigning seat")
	}
	if len(four.Active()) != 3 {
		t.Fatalf("active seats = %d, want 3", len(four.Active()))
	}
}

func TestMoveLimitDraw(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	s.Ply = MaxPlies - 1
	var to NodeID
	for d := range b.LegalMovesFrom(s, "-4,0") {
		to = d
		break
	}
	if _, err := b.ApplyMove(s, West, "-4,0", to); err != nil {
		t.Fatalf("move rejected: %v", err)
	}
	if s.Status != StatusFinished || s.Winner != "" || s.WinReason != ReasonMoveLimit {
		t.Fatalf("move limit draw not applied: %+v", s)
	}
}

// The players throw for themselves: a fresh game opens in its rolling phase
// and nobody may move until the dice have settled.
func TestOpeningRollIsPlayerDriven(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	s := NewState(seats, seats[0])

	if s.Phase != PhaseRoll {
		t.Fatalf("a new game should start in the rolling phase, got %q", s.Phase)
	}
	if len(s.Pending) != 4 {
		t.Fatalf("every seat owes a throw, got %v", s.Pending)
	}
	if _, err := b.ApplyMove(s, seats[0], "-4,0", "-1,1"); err != ErrStillRolling {
		t.Fatalf("moving before the dice: err = %v, want ErrStillRolling", err)
	}

	// Only a seat that still owes a throw may roll, and only once.
	if s.MayRoll(seats[1]) != true {
		t.Fatalf("%s should be able to throw", seats[1])
	}
	vals := []int{4, 6, 2, 6} // seats 1 and 3 tie on 6
	i := 0
	next := func() int { v := vals[i]; i++; return v }
	for _, seat := range seats {
		if _, err := s.RollDie(seat, next); err != nil {
			t.Fatalf("%s could not throw: %v", seat, err)
		}
	}
	if _, err := s.RollDie(seats[0], next); err != ErrNotYourRoll {
		t.Fatalf("throwing twice: err = %v, want ErrNotYourRoll", err)
	}

	// A shared lead sends exactly those seats back to the dice.
	if s.Phase != PhaseRoll {
		t.Fatalf("a tie must not settle the opening")
	}
	if len(s.Dice) != 2 || len(s.Pending) != 2 {
		t.Fatalf("tie should open a second round for two seats, got %d rounds / pending %v",
			len(s.Dice), s.Pending)
	}
	for _, seat := range s.Pending {
		if seat != seats[1] && seat != seats[3] {
			t.Fatalf("only the tied seats re-roll, found %s", seat)
		}
	}

	tie := []int{3, 5} // seats[3] wins the re-roll
	j := 0
	for _, seat := range append([]Color(nil), s.Pending...) {
		if _, err := s.RollDie(seat, func() int { v := tie[j]; j++; return v }); err != nil {
			t.Fatalf("re-roll for %s failed: %v", seat, err)
		}
	}
	if s.Phase != PhasePlay {
		t.Fatalf("the game should be playable once the tie breaks, phase=%q", s.Phase)
	}
	if s.Turn != seats[3] {
		t.Fatalf("highest re-roll opens: turn = %s, want %s", s.Turn, seats[3])
	}
	if len(s.Pending) != 0 {
		t.Fatalf("nothing should still be owed, got %v", s.Pending)
	}
	if _, err := s.RollDie(seats[0], next); err != ErrNotRolling {
		t.Fatalf("rolling after the opening: err = %v, want ErrNotRolling", err)
	}
	moved := false
	for _, p := range s.Pieces {
		if p.Owner != seats[3] {
			continue
		}
		for to := range b.LegalMovesFrom(s, p.Node) {
			if _, err := b.ApplyMove(s, seats[3], p.Node, to); err != nil {
				t.Fatalf("the winner should be able to move: %v", err)
			}
			moved = true
			break
		}
		if moved {
			break
		}
	}
	if !moved {
		t.Fatalf("the seat that won the roll had no legal move")
	}
}

// Real dice only ever show 1..6.
func TestRollDieRange(t *testing.T) {
	seats := SeatOrder(2)
	for i := 0; i < 50; i++ {
		s := NewState(seats, seats[0])
		for _, seat := range seats {
			v, err := s.RollDie(seat, func() int { return 3 })
			if err != nil {
				t.Fatalf("throw failed: %v", err)
			}
			if v < 1 || v > 6 {
				t.Fatalf("die showed %d", v)
			}
		}
	}
}

func keys(m map[NodeID][]NodeID) []NodeID {
	out := make([]NodeID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
