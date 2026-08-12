package main

import (
	"context"
	"database/sql"
	_ "github.com/lib/pq"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := loadConfig()
	if err := validateConfig(cfg); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
	if err := ApplySchema(ctx, db); err != nil {
		log.Fatal(err)
	}
	if cfg.Identity.RulesEnabled {
		identityRepo := NewSQLIdentityRepository(db)
		if err := identityRepo.EnsureShadowActivation(ctx, cfg.Identity, time.Now().UTC()); err != nil {
			log.Fatal(err)
		}
		if err := identityRepo.ActivateShadowRules(ctx); err != nil {
			log.Fatal(err)
		}
	}
	monitorRuntime, err := newAccountMonitorRuntime(ctx, cfg, db)
	if err != nil {
		log.Fatal(err)
	}
	defer monitorRuntime.Close()
	monitorHandlers := NewHTTPServer(cfg, NewSQLRepository(db))
	if monitorRuntime != nil {
		monitorHandlers.publicGroups = monitorRuntime.source
		monitorHandlers.monitor = monitorRuntime.handler
		if monitorRuntime.collector != nil {
			go monitorRuntime.collector.Run(ctx)
		}
	}
	server := &http.Server{Addr: cfg.Listen, Handler: monitorHandlers, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Printf("extensions-self listening on %s mode=%s", cfg.Listen, cfg.Mode)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
