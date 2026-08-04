// Package httpapi exposes the Kingsreach REST API and serves the static
// frontend (Unity WebGL build) plus an embedded HTML dev client at /dev.
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"math/big"
	"mime"
	"net/http"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"

	"kingsreach/internal/game"
	"kingsreach/internal/store"
)

//go:embed devclient.html
var devClientHTML []byte

//go:embed landing.html
var landingHTML []byte

func init() {
	// Make sure Unity WebGL artifacts get sensible types everywhere.
	_ = mime.AddExtensionType(".wasm", "application/wasm")
	_ = mime.AddExtensionType(".data", "application/octet-stream")
	_ = mime.AddExtensionType(".unityweb", "application/octet-stream")
}

type Server struct {
	board     *game.Board
	st        store.Store
	staticDir string
	mux       *http.ServeMux
	boardJSON []byte
	locks     sync.Map // gameID -> *sync.Mutex, serializes writes per game

	// Steam sign-in; nil when no realm is configured.
	steam *SteamAuth
	// Country guesses per address, so we ask at most once per player.
	geo *geoCache
	// BotLobbies keeps computer-hosted tables open in the browser.
	BotLobbies bool
	seedMu     sync.Mutex
	lastSeed   time.Time
	seedTarget int
}

func New(st store.Store, staticDir string) *Server {
	s := &Server{
		board: game.BuildBoard(), st: st, staticDir: staticDir,
		mux: http.NewServeMux(), BotLobbies: true, geo: newGeoCache(),
	}

	type boardOut struct {
		Nodes []*game.Node `json:"nodes"`
		Edges []game.Edge  `json:"edges"`
	}
	s.boardJSON, _ = json.Marshal(boardOut{Nodes: s.board.NodeList(), Edges: s.board.Edges})

	s.mux.HandleFunc("POST /api/games", s.handleCreate)
	s.mux.HandleFunc("POST /api/games/join", s.handleJoin)
	s.mux.HandleFunc("POST /api/games/{id}/start", s.handleLobbyStart)
	s.mux.HandleFunc("GET /api/lobbies", s.handleLobbies)
	s.mux.HandleFunc("GET /api/board", s.handleBoard)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/geo", s.handleGeo)
	s.mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	s.mux.HandleFunc("POST /api/profile", s.handleProfileCreate)
	s.mux.HandleFunc("GET /api/profile", s.handleProfileGet)
	s.mux.HandleFunc("POST /api/profile/update", s.handleProfileUpdate)
	s.mux.HandleFunc("POST /api/profile/equip", s.handleProfileEquip)
	s.mux.HandleFunc("GET /api/auth/steam/login", s.handleSteamLogin)
	s.mux.HandleFunc("GET /api/auth/steam/return", s.handleSteamReturn)
	s.mux.HandleFunc("GET /api/games/{id}", s.handleGetState)
	s.mux.HandleFunc("GET /api/games/{id}/moves", s.handleMoves)
	s.mux.HandleFunc("POST /api/games/{id}/move", s.handleMove)
	s.mux.HandleFunc("POST /api/games/{id}/roll", s.handleRoll)
	s.mux.HandleFunc("POST /api/games/{id}/resign", s.handleResign)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("/", s.handleStatic)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Permissive CORS: the API carries per-game secrets, not accounts, and
	// this makes editor/dev-server frontends painless.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// EnableSteam turns on Steam sign-in. realm is the public address players
// reach the game on, e.g. https://kingsreach.example.com.
func (s *Server) EnableSteam(realm string) {
	s.steam = NewSteamAuth(realm)
}

func (s *Server) lock(gameID string) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(gameID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randCode() string {
	out := make([]byte, 5)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			panic(err)
		}
		out[i] = codeAlphabet[n.Int64()]
	}
	return string(out)
}

// ---- client-facing state shape ----

type clientPiece struct {
	ID       string `json:"id"`
	Owner    string `json:"owner"`
	Value    int    `json:"value"`
	Node     string `json:"node"`
	Captured bool   `json:"captured"`
}

type clientMove struct {
	Piece    string   `json:"piece"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	Path     []string `json:"path"`
	Captured string   `json:"captured"`
}

type clientSeat struct {
	Seat    string `json:"seat"`
	Skin    string `json:"skin"`
	Taken   bool   `json:"taken"`
	You     bool   `json:"you"`
	Out     bool   `json:"out"`
	Cause   string `json:"cause,omitempty"`
	Name    string `json:"name,omitempty"`
	Avatar  string `json:"avatar,omitempty"`
	Country string `json:"country,omitempty"`
}

type clientState struct {
	GameID    string             `json:"gameId"`
	Code      string             `json:"code"`
	Name      string             `json:"name"` // table name, as shown in the browser
	Mode      string             `json:"mode"`
	Status    string             `json:"status"`
	Phase     string             `json:"phase"` // "roll" | "play"
	Players   int                `json:"players"`
	Seats     []clientSeat       `json:"seats"`
	You       string             `json:"you"` // your seat, or "all" when you run the table
	Turn      string             `json:"turn"`
	Winner    string             `json:"winner"`
	WinReason string             `json:"winReason"`
	Version   int                `json:"version"`
	Ply       int                `json:"ply"`
	Capturing bool               `json:"capturing"` // strikes allowed yet?
	Dice      [][]game.DiceThrow `json:"dice,omitempty"`
	Pending   []string           `json:"pending,omitempty"` // seats still owing a throw
	YourRoll  bool               `json:"yourRoll"`          // may you throw right now?
	Pieces    []clientPiece      `json:"pieces"`
	LastMove  *clientMove        `json:"lastMove"`
}

// stateWithRoll adds the die just thrown, so the client animates that value.
type stateWithRoll struct {
	*clientState
	RolledSeat string `json:"rolledSeat"`
	Rolled     int    `json:"rolled"`
}

func toClientState(rec *store.GameRecord, token string) *clientState {
	st := rec.State
	you := ""
	if rec.HoldsEverySeat(token) {
		you = "all"
	} else if seat, ok := rec.SeatFor(token); ok {
		you = string(seat.Seat)
	}

	out := &clientState{
		GameID: rec.ID, Code: rec.Code, Name: rec.Name, Mode: rec.Mode, Status: rec.Status,
		Phase: st.Phase, Players: rec.Players, You: you, Turn: string(st.Turn),
		Winner: string(st.Winner), WinReason: st.WinReason,
		Version: rec.Version, Ply: st.Ply, Capturing: st.CapturesAllowed(),
		Dice: st.Dice,
	}
	for _, seat := range st.Pending {
		out.Pending = append(out.Pending, string(seat))
		if you == "all" || string(seat) == you {
			out.YourRoll = true
		}
	}
	for _, seat := range rec.Seats {
		cs := clientSeat{
			Seat: string(seat.Seat), Skin: seat.Skin, Taken: seat.Taken,
			You: seat.Taken && seat.Token == token, Out: st.IsOut(seat.Seat),
			Name: seat.Name, Avatar: seat.Avatar, Country: seat.Country,
		}
		for _, k := range st.Out {
			if k.Seat == seat.Seat {
				cs.Cause = k.Cause
			}
		}
		out.Seats = append(out.Seats, cs)
	}
	for _, p := range st.Pieces {
		out.Pieces = append(out.Pieces, clientPiece{
			ID: string(p.ID), Owner: string(p.Owner), Value: p.Value,
			Node: string(p.Node), Captured: p.Captured,
		})
	}
	if st.LastMove != nil {
		mv := &clientMove{
			Piece: string(st.LastMove.Piece), From: string(st.LastMove.From),
			To: string(st.LastMove.To), Captured: string(st.LastMove.Captured),
		}
		for _, n := range st.LastMove.Path {
			mv.Path = append(mv.Path, string(n))
		}
		out.LastMove = mv
	}
	return out
}

type stateWithToken struct {
	*clientState
	Token string `json:"token"`
}

// ---- static files ----

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/dev" || r.URL.Path == "/dev/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(devClientHTML)
		return
	}
	// URL paths always use forward slashes: clean with package path (not
	// filepath, whose separators are OS-specific) before touching the disk.
	rel := strings.TrimPrefix(stdpath.Clean("/"+r.URL.Path), "/")
	if rel == "" || rel == "." {
		rel = "index.html"
	}
	full := filepath.Join(s.staticDir, filepath.FromSlash(rel))
	if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
		// Prefer a pre-compressed sibling (client build writes .gz next to the
		// bundle) so the big JS payload ships at a fraction of its size.
		if acceptsGzip(r) {
			if gz, err := os.Stat(full + ".gz"); err == nil && !gz.IsDir() {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Vary", "Accept-Encoding")
				w.Header().Set("Content-Type", typeOfInner(full))
				http.ServeFile(w, r, full+".gz")
				return
			}
		}
		// Artifacts requested by their own .gz/.br name still need the headers.
		if strings.HasSuffix(full, ".gz") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", typeOfInner(strings.TrimSuffix(full, ".gz")))
		} else if strings.HasSuffix(full, ".br") {
			w.Header().Set("Content-Encoding", "br")
			w.Header().Set("Content-Type", typeOfInner(strings.TrimSuffix(full, ".br")))
		}
		http.ServeFile(w, r, full)
		return
	}
	if rel == "index.html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(landingHTML)
		return
	}
	http.NotFound(w, r)
}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

func typeOfInner(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func logf(format string, args ...any) { log.Printf(format, args...) }
