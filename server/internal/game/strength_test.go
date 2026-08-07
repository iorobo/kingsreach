package game

import (
	"math/rand"
	"testing"
)

// Does the harder setting actually play better?
//
// "The AI is bad" is an opinion until something measures it. These play the
// levels against each other and report the score, so a change to the
// evaluation is answerable with a number rather than a feeling.

// playOut runs one game between two levels and returns the winner.
func playOut(t *testing.T, b *Board, white, black string, seed int64) Color {
	t.Helper()
	s := started(SeatOrder(2), West)
	rng := rand.New(rand.NewSource(seed))
	level := map[Color]string{West: white, East: black}
	for s.Status == StatusActive && s.Ply < MaxPlies {
		seat := s.Turn
		from, to, ok := b.ChooseMove(s, seat, level[seat], rng)
		if !ok {
			break // walled in; the engine knocks them out on the turn change
		}
		if _, err := b.ApplyMove(s, seat, from, to); err != nil {
			t.Fatalf("%s (%s) played an illegal move %s->%s: %v", seat, level[seat], from, to, err)
		}
	}
	return s.Winner
}

// score plays both colours so neither level gets the opening advantage.
func score(t *testing.T, b *Board, strong, weak string, games int) (strongWins, weakWins, draws int) {
	t.Helper()
	for i := 0; i < games; i++ {
		var winner Color
		var strongSeat Color
		if i%2 == 0 {
			strongSeat = West
			winner = playOut(t, b, strong, weak, int64(i+1))
		} else {
			strongSeat = East
			winner = playOut(t, b, weak, strong, int64(i+1))
		}
		switch winner {
		case "":
			draws++
		case strongSeat:
			strongWins++
		default:
			weakWins++
		}
	}
	return
}

func TestHardBeatsEasy(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole games; skipped under -short")
	}
	b := board(t)
	const games = 10
	hard, easy, draws := score(t, b, Hard, Easy, games)
	t.Logf("hard %d – easy %d (%d drawn) over %d games", hard, easy, draws, games)
	if hard <= easy {
		t.Fatalf("hard won %d and easy won %d — the difficulty levels are not doing anything", hard, easy)
	}
}

func TestMediumBeatsEasy(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole games; skipped under -short")
	}
	b := board(t)
	const games = 10
	medium, easy, draws := score(t, b, Medium, Easy, games)
	t.Logf("medium %d – easy %d (%d drawn) over %d games", medium, easy, draws, games)
	if medium <= easy {
		t.Fatalf("medium won %d and easy won %d", medium, easy)
	}
}

// The bug that made the old bot feel awful: it would step a piece onto a
// square an enemy could take, for no compensation, because it only ever
// checked whether its *king* was hanging.
func TestBotDoesNotGiveAwayAPieceForNothing(t *testing.T) {
	b := board(t)
	// A west 2 can step to -2,2 where an east 3 takes it for free, or to a
	// quiet square. Nothing about the position rewards the sacrifice.
	s := duel(West,
		pc("wK", West, 1, "-8,0"),
		pc("w2a", West, 2, "-4,0"),
		pc("e3a", East, 3, "-2,2"),
		pc("eK", East, 1, "8,0"),
	)
	s.Ply = 4 // past the opening, so strikes are live

	free := NodeID("")
	for to := range b.LegalMovesFrom(s, "-4,0") {
		trial := s.Clone()
		if _, err := b.ApplyMove(trial, West, "-4,0", to); err != nil {
			continue
		}
		if attackers, defenders := b.tradeAt(trial, West, to); attackers > 0 && defenders == 0 {
			free = to
			break
		}
	}
	if free == "" {
		t.Skip("no square in this position hangs the piece; nothing to test")
	}

	for _, level := range []string{Medium, Hard} {
		_, to, ok := b.ChooseMove(s, West, level, rand.New(rand.NewSource(3)))
		if !ok {
			t.Fatalf("%s found no move", level)
		}
		if to == free {
			t.Errorf("%s moved to %s, where the piece is taken for free", level, to)
		}
	}
}

// Easy has to be beatable but not broken: it should still finish games and
// still take a king left in the open most of the time.
func TestEasyIsWeakButNotBroken(t *testing.T) {
	b := board(t)
	s := started(SeatOrder(2), West)
	rng := rand.New(rand.NewSource(11))
	for s.Status == StatusActive && s.Ply < MaxPlies {
		from, to, ok := b.ChooseMove(s, s.Turn, Easy, rng)
		if !ok {
			break
		}
		if _, err := b.ApplyMove(s, s.Turn, from, to); err != nil {
			t.Fatalf("easy played an illegal move %s->%s: %v", from, to, err)
		}
	}
	if s.Status != StatusFinished {
		t.Fatalf("two easy bots never finished a game (ply %d)", s.Ply)
	}
}
