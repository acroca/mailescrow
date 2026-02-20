package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/albert/mailescrow/internal/config"
	"github.com/albert/mailescrow/internal/imap"
	"github.com/albert/mailescrow/internal/relay"
	"github.com/albert/mailescrow/internal/store"
	"github.com/albert/mailescrow/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	st, err := store.New(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	r := relay.New(cfg.Relay.Host, cfg.Relay.Port, cfg.Relay.Username, cfg.Relay.Password, cfg.Relay.TLS)

	ctx := context.Background()

	var imapClient *imap.Client
	if cfg.IMAP.Host != "" {
		imapClient = imap.New(cfg.IMAP.Host, cfg.IMAP.Port, cfg.IMAP.Username, cfg.IMAP.Password, cfg.IMAP.TLS)

		if err := imapClient.EnsureFolders(ctx); err != nil {
			return fmt.Errorf("ensure IMAP folders: %w", err)
		}
		log.Printf("IMAP folders verified on %s", cfg.IMAP.Host)

		go runIMAPPoller(ctx, imapClient, st, cfg.IMAP.PollInterval)
	} else {
		log.Printf("IMAP not configured; inbound polling disabled")
	}

	webSrv := web.New(st, r, imapClient, cfg.Relay.Username, cfg.Relay.FromName)

	go func() {
		if err := webSrv.Serve(cfg.Web.Listen); err != nil {
			log.Fatalf("Web UI error: %v", err)
		}
	}()

	go func() {
		if err := webSrv.ServeAPI(cfg.Web.APIListen); err != nil {
			log.Fatalf("API server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	if err := webSrv.Shutdown(context.Background()); err != nil {
		log.Printf("Web server shutdown: %v", err)
	}
	log.Println("Stopped")
	return nil
}

func runIMAPPoller(ctx context.Context, client *imap.Client, st store.EmailStore, interval time.Duration) {
	log.Printf("IMAP poller started (interval: %s)", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	poll := func() {
		emails, err := st.ListPending(ctx)
		if err != nil {
			log.Printf("IMAP poll: list pending: %v", err)
			return
		}

		knownIDs := make([]string, 0, len(emails))
		for _, e := range emails {
			if e.IMAPMessageID != "" {
				knownIDs = append(knownIDs, e.IMAPMessageID)
			}
		}

		// Also collect known IDs from approved (not yet fetched) emails.
		approved, err := st.ListApproved(ctx)
		if err != nil {
			log.Printf("IMAP poll: list approved: %v", err)
		} else {
			for _, e := range approved {
				if e.IMAPMessageID != "" {
					knownIDs = append(knownIDs, e.IMAPMessageID)
				}
			}
		}

		fetched, err := client.Poll(ctx, knownIDs)
		if err != nil {
			log.Printf("IMAP poll error: %v", err)
			return
		}

		for _, f := range fetched {
			id, err := st.SaveInbound(ctx, f.Sender, f.Recipients, f.Subject, f.Body, f.RawMessage, f.MessageID, imap.FolderReceived)
			if err != nil {
				log.Printf("IMAP poll: save inbound: %v", err)
				continue
			}
			log.Printf("Received inbound email %s from %s (subject: %s)", id, f.Sender, f.Subject)
		}
	}

	// Poll immediately on startup.
	poll()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}
