package game

import (
	"sort"
	"testing"
)

// anyMove picks a legal destination for the stone on `from`, so a test about
// inheritance is not also a test of whether the author can do hex arithmetic
// in their head. Sorted, so a failure is reproducible.
func anyMove(t *testing.T, b *Board, s *State, from NodeID) NodeID {
	t.Helper()
	moves := b.LegalMovesFrom(s, from)
	if len(moves) == 0 {
		t.Fatalf("no legal move from %s", from)
	}
	dests := make([]string, 0, len(moves))
	for to := range moves {
		dests = append(dests, string(to))
	}
	sort.Strings(dests)
	return NodeID(dests[0])
}

// anyMoveFor finds some legal move for a seat. The opening king is boxed in by
// its own guard, so "move the king" is not a move a test can assume.
func anyMoveFor(t *testing.T, b *Board, s *State, seat Color) (from, to NodeID) {
	t.Helper()
	froms := []string{}
	for _, p := range s.Pieces {
		if !p.Captured && p.Owner == seat && len(b.LegalMovesFrom(s, p.Node)) > 0 {
			froms = append(froms, string(p.Node))
		}
	}
	if len(froms) == 0 {
		t.Fatalf("%s has no legal move at all", seat)
	}
	sort.Strings(froms)
	return NodeID(froms[0]), anyMove(t, b, s, NodeID(froms[0]))
}

// Team play: two against two, with and without friendly fire, and the option
// that hands a fallen player's stones to their partner.
//
// The thing worth testing hardest is not that allies cannot hit each other —
// that is one comparison — but that the *game ends at the right moment*. In a
// free-for-all the rule was "one player left"; with sides it has to be "one
// side left", and a team game that keeps going with two survivors on the same
// side would be a very quiet bug: nobody can win, and the move limit
// eventually calls it a draw two hundred plies later.

// pair builds a four-seat table with the opposite players partnered.
func pair(turn Color, friendlyFire, inherit bool, pieces ...*Piece) *State {
	seats := SeatOrder(4)
	s := craft(seats, turn, pieces...)
	s.SetTeams(PairedTeams(seats), friendlyFire, inherit)
	return s
}

func TestPairedTeamsAlternateSides(t *testing.T) {
	seats := SeatOrder(4)
	teams := PairedTeams(seats)
	if len(teams) != 4 {
		t.Fatalf("PairedTeams gave %d entries, want 4", len(teams))
	}
	// Turn order must alternate, or a "partnership" is just two players moving
	// twice in a row.
	for i, seat := range seats {
		next := seats[(i+1)%len(seats)]
		if teams[seat] == teams[next] {
			t.Fatalf("%s and %s play consecutively and are on the same side", seat, next)
		}
	}
	// And partners must sit opposite.
	if teams[seats[0]] != teams[seats[2]] || teams[seats[1]] != teams[seats[3]] {
		t.Fatalf("partners are not sitting opposite each other: %v", teams)
	}
	if PairedTeams(SeatOrder(3)) != nil {
		t.Fatal("three players cannot be split into two sides")
	}
}

func TestAlliesCannotStrikeEachOther(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	ally := seats[2] // West's partner
	s := pair(West, false, false,
		pc("wK", West, 1, "-8,0"),
		pc("w2a", West, 2, "-4,0"),
		pc("aP", ally, 3, "-1,1"),
		pc("aK", ally, 1, "8,0"),
		pc("xK", seats[1], 1, "-4,-8"),
		pc("yK", seats[3], 1, "4,8"),
	)
	if _, ok := b.LegalMovesFrom(s, "-4,0")["-1,1"]; ok {
		t.Fatal("a 3-step move onto a partner's stone was offered without friendly fire")
	}
	if _, err := b.ApplyMove(s, West, "-4,0", "-1,1"); err == nil {
		t.Fatal("striking a partner was allowed without friendly fire")
	}
}

func TestFriendlyFireAllowsIt(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	ally := seats[2]
	s := pair(West, true, false,
		pc("wK", West, 1, "-8,0"),
		pc("w2a", West, 2, "-4,0"),
		pc("aP", ally, 3, "-1,1"),
		pc("aK", ally, 1, "8,0"),
		pc("xK", seats[1], 1, "-4,-8"),
		pc("yK", seats[3], 1, "4,8"),
	)
	if _, ok := b.LegalMovesFrom(s, "-4,0")["-1,1"]; !ok {
		t.Fatal("friendly fire is on but the partner's stone is not a target")
	}
	if _, err := b.ApplyMove(s, West, "-4,0", "-1,1"); err != nil {
		t.Fatalf("striking a partner under friendly fire: %v", err)
	}
	if p := s.PieceByID("aP"); !p.Captured {
		t.Fatal("the partner's stone survived a strike that was allowed")
	}
}

// The whole point of sides: the game is over when one side is left, not when
// one player is.
func TestGameEndsWhenOneSideIsLeft(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	west, foeA, ally, foeB := seats[0], seats[1], seats[2], seats[3]
	s := pair(West, false, false,
		pc("wK", west, 1, "-8,0"),
		pc("w2a", west, 2, "-5,-1"),
		pc("aK", ally, 1, "8,0"),
		pc("xK", foeA, 1, "4,-8"),
		pc("yK", foeB, 1, "4,8"),
	)
	s.knockOut(foeB, OutResigned)
	if s.Status != StatusActive {
		t.Fatalf("one opponent down should not end a 2v2, status=%s", s.Status)
	}
	// Put the last rival king somewhere the 2 can actually reach, rather than
	// trusting hand-computed hex coordinates.
	target := anyMove(t, b, s, "-5,-1")
	s.PieceByID("xK").Node = target

	// Take it. Two players are still on the board, but they are partners, so
	// that is the end of it.
	if _, err := b.ApplyMove(s, west, "-5,-1", target); err != nil {
		t.Fatalf("capturing the last rival king on %s: %v", target, err)
	}
	if s.Status != StatusFinished {
		t.Fatalf("status=%s, want finished — only one side is still standing", s.Status)
	}
	if s.WinReason != ReasonLastAlive {
		t.Fatalf("reason=%q, want %q", s.WinReason, ReasonLastAlive)
	}
	side := s.WinningSide()
	if len(side) != 2 {
		t.Fatalf("WinningSide gave %v, want both partners", side)
	}
	found := map[Color]bool{}
	for _, c := range side {
		found[c] = true
	}
	if !found[west] || !found[ally] {
		t.Fatalf("WinningSide = %v, want %s and %s", side, west, ally)
	}
}

// Without teams nothing above may change: a four-player free-for-all still
// runs until one player is left.
func TestFreeForAllStillNeedsOneSurvivor(t *testing.T) {
	seats := SeatOrder(4)
	s := craft(seats, seats[0], pc("wK", seats[0], 1, "-8,0"), pc("xK", seats[1], 1, "-4,-8"))
	s.knockOut(seats[2], OutResigned)
	s.knockOut(seats[3], OutResigned)
	if s.settleIfOneSideLeft() {
		t.Fatal("two rivals left in a free-for-all and the game called itself over")
	}
	s.knockOut(seats[1], OutResigned)
	if !s.settleIfOneSideLeft() || s.Winner != seats[0] {
		t.Fatalf("last player standing should win; status=%s winner=%s", s.Status, s.Winner)
	}
}

func TestFallenPartnersStonesStayPutByDefault(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	ally := seats[2]
	s := pair(West, false, false,
		pc("wK", West, 1, "-8,0"),
		pc("aP", ally, 2, "-4,0"),
		pc("aK", ally, 1, "8,0"),
		pc("xK", seats[1], 1, "-4,-8"),
		pc("yK", seats[3], 1, "4,8"),
	)
	s.knockOut(ally, OutResigned)
	if got := b.LegalMovesFrom(s, "-4,0"); len(got) != 0 {
		t.Fatalf("a fallen partner's stone offered %d moves; it should be scenery", len(got))
	}
	if _, err := b.ApplyMove(s, West, "-4,0", "-1,1"); err == nil {
		t.Fatal("moved a fallen partner's stone with inheritance switched off")
	}
}

func TestInheritedStonesMoveOnThePartnersTurn(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	ally := seats[2]
	s := pair(West, false, true,
		pc("wK", West, 1, "-8,0"),
		pc("aP", ally, 2, "-4,0"),
		pc("aK", ally, 1, "8,0"),
		pc("xK", seats[1], 1, "-4,-8"),
		pc("yK", seats[3], 1, "4,8"),
	)
	s.knockOut(ally, OutResigned)
	dest := anyMove(t, b, s, "-4,0")
	if _, err := b.ApplyMove(s, West, "-4,0", dest); err != nil {
		t.Fatalf("moving an inherited stone to %s: %v", dest, err)
	}
	if p := s.PieceByID("aP"); p.Node != dest {
		t.Fatalf("inherited stone is on %s, want %s", p.Node, dest)
	}
	// It still belongs to the fallen player; inheriting is not annexing.
	if p := s.PieceByID("aP"); p.Owner != ally {
		t.Fatalf("inherited stone changed owner to %s", p.Owner)
	}
	// And the enemy cannot pick it up.
	if s.MayMove(seats[1], ally) {
		t.Fatal("an opponent inherited the fallen player's stones")
	}
}

// A knocked-out seat never gets a turn of its own, inheritance or not.
//
// Built from the real opening setup rather than a handful of lone kings: the
// question is about turn order, and a seat holding one walled-in king gets
// knocked out for having no move, which would answer a different question.
func TestInheritDoesNotResurrectTheTurnOrder(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	ally := seats[2]
	s := started(seats, West)
	s.SetTeams(PairedTeams(seats), false, true)
	s.knockOut(ally, OutResigned)

	from, dest := anyMoveFor(t, b, s, West)
	if _, err := b.ApplyMove(s, West, from, dest); err != nil {
		t.Fatalf("west move %s->%s: %v", from, dest, err)
	}
	if s.Turn == ally {
		t.Fatal("play passed to a knocked-out seat")
	}
	if s.Turn != seats[1] {
		t.Fatalf("turn went to %s, want %s", s.Turn, seats[1])
	}

	// And when it comes back round to West, the ally's stones are theirs to
	// move — that is the whole feature.
	if !s.MayMove(West, ally) {
		t.Fatal("west cannot move their fallen partner's stones")
	}
}

// Teams are carried through Clone, or the bot searches a position in which
// everybody is suddenly an enemy.
func TestCloneKeepsTheSides(t *testing.T) {
	seats := SeatOrder(4)
	s := pair(West, true, true, pc("wK", West, 1, "-8,0"))
	cp := s.Clone()
	if !cp.Allied(seats[0], seats[2]) {
		t.Fatal("Clone lost the partnership")
	}
	if !cp.FriendlyFire || !cp.Inherit {
		t.Fatal("Clone lost the team options")
	}
	cp.Teams[seats[0]] = 9
	if s.Teams[seats[0]] == 9 {
		t.Fatal("Clone shares the team map with the original")
	}
}
