package game

import (
	"fmt"
	"time"
)

const (
	StatusActive   = "active"
	StatusFinished = "finished"

	// Phases within an active game. Nobody moves until the dice have settled
	// who opens — and the players throw those dice themselves.
	PhaseRoll = "roll"
	PhasePlay = "play"

	ReasonThrone    = "throne"        // a king reached the Throne
	ReasonLastAlive = "last-standing" // every rival has been knocked out
	ReasonResign    = "resign"
	ReasonMoveLimit = "move-limit" // server safeguard: draw after MaxPlies

	// Why a player dropped out (they stay on the board as obstacles).
	OutKingTaken = "king-captured"
	OutBlocked   = "no-legal-move"
	OutResigned  = "resigned"
	// OutTimedOut is its own cause rather than being folded into OutResigned:
	// walking away and giving up are different things, and the seat list says
	// so to the person it happened to.
	OutTimedOut = "out-of-time"
)

// MaxPlies is a server safeguard (house rule): after this many plies the game
// is declared a draw so games cannot run forever.
const MaxPlies = 800

type PieceID string

type Piece struct {
	ID       PieceID `json:"id"`
	Owner    Color   `json:"owner"`
	Value    int     `json:"value"` // 1 = King
	Node     NodeID  `json:"node"`  // empty when captured
	Captured bool    `json:"captured"`
}

type MoveRecord struct {
	Piece    PieceID  `json:"piece"`
	From     NodeID   `json:"from"`
	To       NodeID   `json:"to"`
	Path     []NodeID `json:"path"` // includes From as first element
	Captured PieceID  `json:"captured,omitempty"`
}

// Knockout records a player leaving the game. Kendo keeps their pieces on the
// board afterwards, so the seat is remembered rather than swept away.
type Knockout struct {
	Seat  Color  `json:"seat"`
	Ply   int    `json:"ply"`
	Cause string `json:"cause"`
}

// DiceThrow is one player's roll when deciding who opens.
type DiceThrow struct {
	Seat  Color `json:"seat"`
	Value int   `json:"value"`
}

type State struct {
	Seats     []Color       `json:"seats"` // seating order, clockwise round the board
	Pieces    []*Piece      `json:"pieces"`
	Turn      Color         `json:"turn"`
	Ply       int           `json:"ply"`
	Status    string        `json:"status"`
	Phase     string        `json:"phase"` // "roll" until the dice decide, then "play"
	Winner    Color         `json:"winner,omitempty"`
	WinReason string        `json:"winReason,omitempty"`
	Out       []Knockout    `json:"out,omitempty"`
	Dice      [][]DiceThrow `json:"dice,omitempty"`    // one entry per round; the last may be in progress
	Pending   []Color       `json:"pending,omitempty"` // seats that still have to throw
	LastMove  *MoveRecord   `json:"lastMove,omitempty"`
	// TurnDeadline is when whoever we are waiting on runs out of time. Zero
	// means no clock. The engine only records it; the caller decides how long
	// an action may take and enforces expiry, because only the caller knows
	// which seats are people and which are the computer.
	TurnDeadline time.Time `json:"turnDeadline,omitempty"`
}

// ArmClock gives whoever we are now waiting on a fresh allowance. Called after
// any action, since an action always changes who we are waiting for. A zero or
// negative duration turns the clock off.
func (s *State) ArmClock(now time.Time, d time.Duration) {
	if s.Status != StatusActive || d <= 0 {
		s.TurnDeadline = time.Time{}
		return
	}
	s.TurnDeadline = now.Add(d)
}

// Overdue lists the seats that have run out of time: the seat to move once
// play has started, or everyone who still owes a throw during the opening. It
// returns nothing while the clock is off or still running.
func (s *State) Overdue(now time.Time) []Color {
	if s.Status != StatusActive || s.TurnDeadline.IsZero() || now.Before(s.TurnDeadline) {
		return nil
	}
	if s.Phase == PhaseRoll {
		out := make([]Color, 0, len(s.Pending))
		for _, seat := range s.Pending {
			if !s.IsOut(seat) {
				out = append(out, seat)
			}
		}
		return out
	}
	if s.IsOut(s.Turn) {
		return nil
	}
	return []Color{s.Turn}
}

// TimeOut knocks a player out for taking too long. Same consequence as
// resigning — their stones stay on the board and the last player standing
// wins — so a table with one opponent left simply hands them the game.
func (s *State) TimeOut(b *Board, c Color) error {
	if s.Status != StatusActive {
		return ErrGameOver
	}
	if s.IsOut(c) {
		return ErrNotPlaying
	}
	s.knockOut(c, OutTimedOut)
	s.settleAfterKnockout(b, c)
	return nil
}

// Starting setup for the west player, exactly as in the rules diagram and the
// photo of the physical set: the king on the westernmost field flanked by the
// 3s along the board edge, the 2s on the inner row. Every other seat is this
// same formation rotated about the centre (see seatRotation).
//
// Note: Kendo gives each player 1 prince, 3 samurai and 4 fighters — which is
// what the diagram and the physical set show. The Dutch rules text says 6
// threes; the diagram/photo version is implemented.
var westSetup = []struct {
	Value  int
	KX, KY int
}{
	{1, -8, 0},
	{3, -8, -2}, {3, -7, -1}, {3, -7, 1}, {3, -8, 2},
	{2, -5, -1}, {2, -4, 0}, {2, -5, 1},
}

// How far each seat's home is rotated from west, in 60° steps. The board has
// six-fold symmetry, so a rotated west formation always lands on real fields.
var seatRotation = map[Color]int{
	West:      0,
	SouthWest: 1,
	SouthEast: 2,
	East:      3,
	NorthEast: 4,
	NorthWest: 5,
}

var seatPrefix = map[Color]string{
	West: "w", SouthWest: "s", SouthEast: "d", East: "e", NorthEast: "n", NorthWest: "m",
}

// SeatOrder returns the seats for a player count, in clockwise board order so
// play travels round the table. Two players sit opposite; four players form
// two opposing pairs.
func SeatOrder(players int) []Color {
	switch players {
	case 3:
		return []Color{West, SouthEast, NorthEast}
	case 4:
		return []Color{West, SouthWest, East, NorthEast}
	default:
		return []Color{West, East}
	}
}

// rotate60 turns a board key one sixth of a turn about the centre.
func rotate60(kx, ky int) (int, int) {
	return (kx - 3*ky) / 2, (kx + ky) / 2
}

func rotateN(kx, ky, times int) (int, int) {
	for i := 0; i < times; i++ {
		kx, ky = rotate60(kx, ky)
	}
	return kx, ky
}

// NewState builds the initial position for the given seats; first is the seat
// to move (normally the winner of the opening dice roll).
func NewState(seats []Color, first Color) *State {
	s := &State{
		Seats: append([]Color(nil), seats...), Turn: first, Status: StatusActive,
		// Everyone throws before anyone moves.
		Phase: PhaseRoll, Dice: [][]DiceThrow{{}}, Pending: append([]Color(nil), seats...),
	}
	for _, seat := range seats {
		prefix := seatPrefix[seat]
		turns := seatRotation[seat]
		counts := map[int]int{}
		for _, sp := range westSetup {
			counts[sp.Value]++
			var id PieceID
			if sp.Value == 1 {
				id = PieceID(prefix + "K")
			} else {
				id = PieceID(fmt.Sprintf("%s%d%c", prefix, sp.Value, 'a'+counts[sp.Value]-1))
			}
			kx, ky := rotateN(sp.KX, sp.KY, turns)
			s.Pieces = append(s.Pieces, &Piece{ID: id, Owner: seat, Value: sp.Value, Node: keyID(kx, ky)})
		}
	}
	return s
}

// Clone deep-copies the state so a move can be tried without touching the
// real game — the search in bot.go relies on this.
func (s *State) Clone() *State {
	cp := *s
	cp.Seats = append([]Color(nil), s.Seats...)
	cp.Out = append([]Knockout(nil), s.Out...)
	cp.Pending = append([]Color(nil), s.Pending...)
	cp.Dice = make([][]DiceThrow, len(s.Dice))
	for i, round := range s.Dice {
		cp.Dice[i] = append([]DiceThrow(nil), round...)
	}
	cp.Pieces = make([]*Piece, len(s.Pieces))
	for i, p := range s.Pieces {
		q := *p
		cp.Pieces[i] = &q
	}
	cp.LastMove = nil
	return &cp
}

// Occupancy maps occupied nodes to the piece standing on them.
func (s *State) Occupancy() map[NodeID]*Piece {
	occ := make(map[NodeID]*Piece, len(s.Pieces))
	for _, p := range s.Pieces {
		if !p.Captured {
			occ[p.Node] = p
		}
	}
	return occ
}

func (s *State) PieceByID(id PieceID) *Piece {
	for _, p := range s.Pieces {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *State) King(c Color) *Piece {
	for _, p := range s.Pieces {
		if p.Owner == c && p.Value == 1 {
			return p
		}
	}
	return nil
}

// IsOut reports whether a seat has been knocked out. Their pieces stay on the
// board, so this is the only way to tell a live player from a dead one.
func (s *State) IsOut(c Color) bool {
	for _, k := range s.Out {
		if k.Seat == c {
			return true
		}
	}
	return false
}

func (s *State) Active() []Color {
	out := make([]Color, 0, len(s.Seats))
	for _, seat := range s.Seats {
		if !s.IsOut(seat) {
			out = append(out, seat)
		}
	}
	return out
}

// CapturesAllowed reports whether pieces may be taken yet. Kendo protects the
// opening round: nobody can be struck until everyone has had a turn.
func (s *State) CapturesAllowed() bool {
	return s.Ply >= len(s.Seats)
}

// MayRoll reports whether this seat still owes a throw.
func (s *State) MayRoll(seat Color) bool {
	if s.Status != StatusActive || s.Phase != PhaseRoll {
		return false
	}
	for _, c := range s.Pending {
		if c == seat {
			return true
		}
	}
	return false
}

// maxRollRounds stops a pathological run of ties from looping forever.
const maxRollRounds = 8

// RollDie throws for one seat. Once the round is complete the highest single
// throw opens the game; if the lead is shared, only those seats throw again.
// The value is generated here, at the moment the player throws — the client
// animates the result rather than deciding it.
func (s *State) RollDie(seat Color, roll func() int) (int, error) {
	if s.Status != StatusActive || s.Phase != PhaseRoll {
		return 0, ErrNotRolling
	}
	if !s.MayRoll(seat) {
		return 0, ErrNotYourRoll
	}
	value := roll()
	round := len(s.Dice) - 1
	s.Dice[round] = append(s.Dice[round], DiceThrow{Seat: seat, Value: value})

	pending := s.Pending[:0:0]
	for _, c := range s.Pending {
		if c != seat {
			pending = append(pending, c)
		}
	}
	s.Pending = pending
	if len(s.Pending) == 0 {
		s.settleRollRound()
	}
	return value, nil
}

func (s *State) settleRollRound() {
	round := s.Dice[len(s.Dice)-1]
	best := 0
	for _, t := range round {
		if t.Value > best {
			best = t.Value
		}
	}
	leaders := []Color{}
	for _, t := range round {
		if t.Value == best {
			leaders = append(leaders, t.Seat)
		}
	}
	if len(leaders) == 1 || len(s.Dice) >= maxRollRounds {
		s.Turn = leaders[0]
		s.Phase = PhasePlay
		s.Pending = nil
		return
	}
	// A shared lead: those seats throw again, nobody else.
	s.Dice = append(s.Dice, []DiceThrow{})
	s.Pending = leaders
}

func (s *State) knockOut(seat Color, cause string) {
	if s.IsOut(seat) {
		return
	}
	s.Out = append(s.Out, Knockout{Seat: seat, Ply: s.Ply, Cause: cause})
	// Somebody leaving during the opening throws must not stall the round.
	if s.Phase == PhaseRoll && len(s.Pending) > 0 {
		pending := s.Pending[:0:0]
		for _, c := range s.Pending {
			if c != seat {
				pending = append(pending, c)
			}
		}
		s.Pending = pending
		if len(s.Pending) == 0 {
			s.settleRollRound()
		}
	}
}

func (s *State) finish(winner Color, reason string) {
	s.Status = StatusFinished
	s.Winner = winner
	s.WinReason = reason
}

func (s *State) finishDraw(reason string) {
	s.Status = StatusFinished
	s.Winner = ""
	s.WinReason = reason
}

// nextSeat returns the seat after c in board order, skipping knocked-out
// players; it returns false when nobody is left to move.
func (s *State) nextSeat(c Color) (Color, bool) {
	idx := -1
	for i, seat := range s.Seats {
		if seat == c {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	for step := 1; step <= len(s.Seats); step++ {
		cand := s.Seats[(idx+step)%len(s.Seats)]
		if !s.IsOut(cand) {
			return cand, true
		}
	}
	return "", false
}

// Resign knocks a player out at their own request.
func (s *State) Resign(b *Board, c Color) error {
	if s.Status != StatusActive {
		return ErrGameOver
	}
	if s.IsOut(c) {
		return ErrNotPlaying
	}
	s.knockOut(c, OutResigned)
	if s.settleAfterKnockout(b, c) {
		return nil
	}
	return nil
}

// settleAfterKnockout ends the game if one player is left, and otherwise hands
// the turn on when the seat that just left was the one to move. Reports
// whether the game finished.
func (s *State) settleAfterKnockout(b *Board, leaver Color) bool {
	if active := s.Active(); len(active) <= 1 {
		if len(active) == 1 {
			s.finish(active[0], ReasonLastAlive)
		} else {
			s.finishDraw(ReasonLastAlive)
		}
		return true
	}
	if s.Turn == leaver {
		s.advanceTurn(b, leaver)
	}
	return s.Status == StatusFinished
}
