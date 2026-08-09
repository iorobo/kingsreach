package httpapi

import (
	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

// Seat colour, decoupled from the seat.
//
// A seat used to be a colour: west was Obsidian and that was that. Which is
// fine until two people both want to play green, and one of them is told they
// are playing green next time. Now the seat decides only where you sit and
// where the camera looks from; the colour is a preference carried on the
// profile.
//
// The awkward part is what to do when two players want the same one, and the
// answer the game already had was sitting right there: the opening dice. The
// player who wins the throw picks first, then the next, and so on — so the
// tie-break is a thing that happens at the table rather than a rule about
// account creation dates.

// Colour is one of the six stone colours. The ids match the client's palette
// in seats.ts; the server never renders anything, it only decides who gets
// which name.
type Colour struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Seat string `json:"seat"` // the seat this colour belongs to by default
}

var colours = []Colour{
	{ID: "obsidian", Name: "Obsidian", Seat: string(game.West)},
	{ID: "jade", Name: "Jade", Seat: string(game.SouthWest)},
	{ID: "amber", Name: "Amber", Seat: string(game.SouthEast)},
	{ID: "ember", Name: "Ember", Seat: string(game.East)},
	{ID: "azure", Name: "Azure", Seat: string(game.NorthEast)},
	{ID: "plum", Name: "Plum", Seat: string(game.NorthWest)},
}

// defaultColour is the colour a seat wears when nobody asked for anything.
func defaultColour(seat game.Color) string {
	for _, c := range colours {
		if c.Seat == string(seat) {
			return c.ID
		}
	}
	return colours[0].ID
}

// validColour normalises a requested colour, returning "" for anything we do
// not recognise — an unknown id means "no preference", not an error, because a
// stale client asking for a colour we dropped should still be able to play.
func validColour(id string) string {
	for _, c := range colours {
		if c.ID == id {
			return id
		}
	}
	return ""
}

// colourOf is the colour a seat is actually wearing, falling back to its own
// so that every game played before this existed still renders.
func colourOf(seat *store.Seat) string {
	if seat == nil {
		return ""
	}
	if seat.Colour != "" {
		return seat.Colour
	}
	return defaultColour(seat.Seat)
}

// pickingOrder ranks the seats for choosing colours: the player who won the
// opening throw first, then the rest by what they rolled, ties by board order.
//
// Using the dice for this is the whole point — it is the answer the game gives
// to every other contested question, and it means the tie-break is watchable.
func pickingOrder(rec *store.GameRecord) []game.Color {
	st := rec.State
	best := map[game.Color]int{}
	for _, round := range st.Dice {
		for _, t := range round {
			if t.Value > best[t.Seat] {
				best[t.Seat] = t.Value
			}
		}
	}
	order := make([]game.Color, 0, len(rec.Seats))
	for _, s := range rec.Seats {
		order = append(order, s.Seat)
	}
	// A stable insertion sort keeps board order as the final tie-break.
	rank := func(c game.Color) int {
		if c == st.Turn {
			return 1 << 20 // the winner of the throw picks first, whatever the dice say now
		}
		return best[c]
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && rank(order[j]) > rank(order[j-1]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order
}

// resolveColours hands out the colours once the dice have settled who opens.
//
// Called after every roll and after a table starts; it does nothing until the
// game is actually in play, and nothing again afterwards. Returns whether it
// changed anything, so the caller knows whether to bump the version.
func resolveColours(rec *store.GameRecord) bool {
	if rec.State == nil || rec.State.Phase != game.PhasePlay {
		return false
	}
	for _, s := range rec.Seats {
		if s.Colour != "" {
			return false // already settled
		}
	}

	taken := map[string]bool{}
	assign := func(seat *store.Seat, id string) bool {
		if id == "" || taken[id] {
			return false
		}
		seat.Colour, taken[id] = id, true
		return true
	}

	order := pickingOrder(rec)
	for _, c := range order {
		seat := rec.SeatByColor(c)
		if seat == nil {
			continue
		}
		if assign(seat, validColour(seat.Wants)) {
			continue
		}
		if assign(seat, defaultColour(seat.Seat)) {
			continue
		}
		// Both gone: give them the first free colour rather than nothing.
		for _, col := range colours {
			if assign(seat, col.ID) {
				break
			}
		}
	}
	return true
}
