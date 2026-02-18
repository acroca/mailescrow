package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/albert/mailescrow/internal/config"
	"github.com/albert/mailescrow/internal/relay"
	"github.com/albert/mailescrow/internal/smtp"
	"github.com/albert/mailescrow/internal/store"
	"github.com/albert/mailescrow/internal/web"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	st, err := store.New(cfg.DB.Path)
	if err != nil {
		log.Fatalf("Failed to open store: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	r := relay.New(cfg.Relay.Host, cfg.Relay.Port, cfg.Relay.Username, cfg.Relay.Password, cfg.Relay.TLS)

	smtpSrv := smtp.New(cfg.SMTP.Listen, cfg.SMTP.Username, cfg.SMTP.Password, st)
	webSrv := web.New(st, r)

	go func() {
		log.Printf("SMTP server listening on %s", cfg.SMTP.Listen)
		if err := smtpSrv.ListenAndServe(); err != nil {
			log.Fatalf("SMTP server error: %v", err)
		}
	}()

	go func() {
		if err := webSrv.Serve(cfg.Web.Listen); err != nil {
			log.Fatalf("Web server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	if err := smtpSrv.Close(); err != nil {
		log.Printf("close SMTP server: %v", err)
	}
	if err := webSrv.Shutdown(context.Background()); err != nil {
		log.Printf("Web server shutdown: %v", err)
	}
	log.Println("Stopped")
}
