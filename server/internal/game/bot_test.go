package game

import (
	"math/rand"
	"testing"
)

func botRNG() *rand.Rand { return rand.New(rand.NewSource(7)) }

func TestBotTakesTheThrone(t *testing.T) {
	b := board(t)
	s := duel(West, pc("wK", West, 1, "2,0"), pc("eK", East, 1, "8,0"))
	from, to, ok := b.ChooseMove(s, West, botRNG())
	if !ok {
		t.Fatalf("bot found no move")
	}
	if to != ThroneID {
		t.Fatalf("bot played %s->%s but the Throne was one step away", from, to)
	}
}

func TestBotTakesAnExposedKing(t *testing.T) {
	b := board(t)
	// East's king sits two steps from a west 2, with nothing in the way.
	s := duel(West,
		pc("wK", West, 1, "-8,0"),
		pc("w2a", West, 2, "-5,1"),
		pc("eK", East, 1, "-8,0"),
	)
	// Put the kings somewhere sane: west king out of the way, east king reachable.
	s.PieceByID("wK").Node = "8,2"
	s.PieceByID("eK").Node = "-8,0"
	from, to, ok := b.ChooseMove(s, West, botRNG())
	if !ok {
		t.Fatalf("bot found no move")
	}
	if to != "-8,0" {
		t.Fatalf("bot played %s->%s and left the enemy king standing", from, to)
	}
}

func TestBotDoesNotHangItsKing(t *testing.T) {
	b := board(t)
	// The west king can step to -5,1, where an east 3 on -2,2 would take it.
	// -7,1 is safe. A bot that ignores the reply walks into the strike.
	s := duel(West,
		pc("wK", West, 1, "-8,0"),
		pc("e3a", East, 3, "-2,2"),
		pc("eK", East, 1, "8,0"),
	)
	risky := NodeID("")
	for to := range b.LegalMovesFrom(s, "-8,0") {
		trial := s.Clone()
		if _, err := b.ApplyMove(trial, West, "-8,0", to); err != nil {
			continue
		}
		if b.kingInDanger(trial, West) {
			risky = to
			break
		}
	}
	if risky == "" {
		t.Skip("no square in this position exposes the king; nothing to test")
	}
	_, to, ok := b.ChooseMove(s, West, botRNG())
	if !ok {
		t.Fatalf("bot found no move")
	}
	if to == risky {
		t.Fatalf("bot stepped onto %s where its king can be taken", to)
	}
}

// The bot must be able to carry a whole game without stalling or cheating.
func TestBotsPlayAFullGame(t *testing.T) {
	b := board(t)
	seats := SeatOrder(4)
	s := started(seats, seats[0])
	rng := botRNG()

	for ply := 0; ply < MaxPlies && s.Status == StatusActive; ply++ {
		seat := s.Turn
		from, to, ok := b.ChooseMove(s, seat, rng)
		if !ok {
			t.Fatalf("ply %d: %s had no move but is still in the game", ply, seat)
		}
		if _, err := b.ApplyMove(s, seat, from, to); err != nil {
			t.Fatalf("ply %d: bot played an illegal move %s->%s: %v", ply, from, to, err)
		}
	}
	if s.Status != StatusFinished {
		t.Fatalf("four bots never finished a game (ply %d)", s.Ply)
	}
	if s.WinReason == ReasonMoveLimit {
		t.Fatalf("bots shuffled to the move limit instead of playing for the win")
	}
	t.Logf("bots finished in %d plies: %s won by %s", s.Ply, s.Winner, s.WinReason)
}

// A bot should beat a player that just shuffles its nearest piece about.
func TestBotBeatsAimlessOpponent(t *testing.T) {
	b := board(t)
	wins := 0
	const games = 6
	for g := 0; g < games; g++ {
		s := started(SeatOrder(2), West) // west = bot, east = aimless
		rng := rand.New(rand.NewSource(int64(g + 1)))
		for s.Status == StatusActive && s.Ply < 400 {
			if s.Turn == West {
				from, to, ok := b.ChooseMove(s, West, rng)
				if !ok {
					break
				}
				if _, err := b.ApplyMove(s, West, from, to); err != nil {
					t.Fatalf("bot move rejected: %v", err)
				}
				continue
			}
			// Aimless: first legal move it finds.
			played := false
			for _, p := range s.Pieces {
				if p.Captured || p.Owner != East {
					continue
				}
				for to := range b.LegalMovesFrom(s, p.Node) {
					if _, err := b.ApplyMove(s, East, p.Node, to); err == nil {
						played = true
					}
					break
				}
				if played {
					break
				}
			}
			if !played {
				break
			}
		}
		if s.Winner == West {
			wins++
		}
	}
	if wins < games-1 {
		t.Fatalf("bot won only %d of %d against an aimless opponent", wins, games)
	}
}
