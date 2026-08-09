package httpapi

import (
	"encoding/json"
	"net/http"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

// Invitations, and the list of tables you are already sitting at.
//
// An invitation is a row and nothing else: no push, no socket, no presence
// protocol. The client is already polling, so it asks for its invitations the
// same way it asks for everything else. The row is joined against the game
// when read, which means an invitation to a table that has since filled up or
// been swept simply stops existing rather than needing a sweeper of its own.

type clientInvite struct {
	ID    string `json:"id"`
	Game  string `json:"gameId"`
	From  string `json:"from"`
	Table string `json:"table"`
	Seats int    `json:"seats"` // free seats left, so "full" is visible before clicking
}

// handleInvite asks a friend to a table the caller is hosting.
func (s *Server) handleInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string `json:"token"` // the *profile* token, not a seat token
		GameID  string `json:"gameId"`
		Profile string `json:"profile"` // who to invite
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	from, err := s.st.GetProfileByToken(r.Context(), req.Token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	rec, err := s.st.Get(r.Context(), req.GameID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "that table is gone")
		return
	}
	// Only somebody at the table may invite to it. Otherwise the endpoint is a
	// way to send any player a message about any table.
	if _, seated := rec.SeatForProfile(from.ID); !seated {
		writeErr(w, http.StatusForbidden, "you are not at that table")
		return
	}
	if rec.Status != "waiting" || rec.FreeSeats() == 0 {
		writeErr(w, http.StatusConflict, "there is no free seat to invite anyone to")
		return
	}
	if req.Profile == "" || req.Profile == from.ID {
		writeErr(w, http.StatusBadRequest, "who are you inviting?")
		return
	}
	if _, already := rec.SeatForProfile(req.Profile); already {
		writeErr(w, http.StatusConflict, "they are already at this table")
		return
	}

	inv := &store.Invite{
		ID: randHex(10), GameID: rec.ID, FromID: from.ID, FromName: from.Name,
		ToID: req.Profile, TableName: rec.Name,
	}
	if err := s.st.CreateInvite(r.Context(), inv); err != nil {
		logf("invite failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not send that invitation")
		return
	}
	logf("game %s: %s invited %s", rec.ID, from.ID, req.Profile)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleInvites lists the invitations waiting for the caller.
func (s *Server) handleInvites(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProfileByToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	invites, err := s.st.InvitesFor(r.Context(), p.ID)
	if err != nil {
		logf("invite list failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read your invitations")
		return
	}
	out := make([]clientInvite, 0, len(invites))
	for _, inv := range invites {
		ci := clientInvite{ID: inv.ID, Game: inv.GameID, From: inv.FromName, Table: inv.TableName}
		if rec, err := s.st.Get(r.Context(), inv.GameID); err == nil {
			ci.Seats = rec.FreeSeats()
			ci.Table = rec.Name // the host may have renamed it since
		}
		out = append(out, ci)
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

// handleInviteDismiss throws an invitation away. Accepting one is just joining
// the table, which already has an endpoint — this is the other button.
func (s *Server) handleInviteDismiss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request body")
		return
	}
	p, err := s.st.GetProfileByToken(r.Context(), req.Token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	if err := s.st.DeleteInvite(r.Context(), req.ID, p.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not dismiss that")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type clientTable struct {
	GameID string `json:"gameId"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Seat   string `json:"seat"`
	Token  string `json:"token"` // the seat token, so the client can walk straight back in
	Yours  bool   `json:"yours"` // is it your move?
	Ply    int    `json:"ply"`
	Free   int    `json:"free"`
}

// handleMyTables lists the unfinished tables the caller holds a seat at.
//
// This is what makes several games at once usable. The seat token comes back
// with each row: a seat is remembered against the profile, so a player who
// closed the tab can be handed the token again rather than being told the
// table is full — the same reasoning as the rejoin path in handleJoin.
func (s *Server) handleMyTables(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProfileByToken(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown profile")
		return
	}
	games, err := s.st.ListForProfile(r.Context(), p.ID, 12)
	if err != nil {
		logf("table list failed: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read your tables")
		return
	}
	out := make([]clientTable, 0, len(games))
	for _, rec := range games {
		seat, ok := rec.SeatForProfile(p.ID)
		if !ok {
			continue
		}
		out = append(out, clientTable{
			GameID: rec.ID, Name: rec.Name, Status: rec.Status,
			Seat: string(seat.Seat), Token: seat.Token,
			Yours: rec.Status == "active" && waitingOn(rec, seat.Seat),
			Ply:   rec.State.Ply, Free: rec.FreeSeats(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": out})
}

// waitingOn reports whether the table is waiting on this seat — to move, or to
// throw its opening die.
func waitingOn(rec *store.GameRecord, seat game.Color) bool {
	if rec.State.Phase == game.PhaseRoll {
		return rec.State.MayRoll(seat)
	}
	return rec.State.Turn == seat
}
