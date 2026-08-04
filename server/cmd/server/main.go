// Kingsreach game server: authoritative rules engine + REST API + static
// hosting for the Unity WebGL frontend. See PROMPT.md at the repo root.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"kingsreach/internal/httpapi"
	"kingsreach/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func main() {
	port := env("PORT", "8080")
	staticDir := env("STATIC_DIR", "web")
	dbURL := os.Getenv("DATABASE_URL")

	if truthy(os.Getenv("KINGSREACH_UNLOCK_ALL")) {
		httpapi.UnlockAll = true
		log.Println("DEV MODE: every skin and environment is unlocked for all profiles")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var st store.Store
	if dbURL == "" {
		log.Println("WARNING: DATABASE_URL is empty -> using in-memory store; games are lost on restart")
		st = store.NewMemory()
	} else {
		pg, err := store.NewPostgres(ctx, dbURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		st = pg
	}
	defer st.Close()

	api := httpapi.New(st, staticDir)

	// Steam sign-in needs to know the address players reach us on, because
	// that is what Steam sends them back to.
	if realm := os.Getenv("PUBLIC_URL"); realm != "" {
		api.EnableSteam(realm)
		log.Printf("Steam sign-in enabled for %s", realm)
	} else {
		log.Println("PUBLIC_URL not set -> Steam sign-in disabled; players can still play as guests")
	}
	if os.Getenv("KINGSREACH_BOT_LOBBIES") == "0" {
		api.BotLobbies = false
		log.Println("computer-hosted tables disabled")
	}
	go api.RunBots(ctx)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("Kingsreach listening on :%s (store=%s, static=%s)", port, st.Name(), staticDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Println("bye")
}
