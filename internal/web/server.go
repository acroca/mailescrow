package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/albert/mailescrow/internal/relay"
	"github.com/albert/mailescrow/internal/store"
	"github.com/google/uuid"
)

//go:embed templates/index.html
var indexHTML string

const (
	folderReceived = "mailescrow/received"
	folderApproved = "mailescrow/approved"
	folderRejected = "mailescrow/rejected"
	folderRead     = "mailescrow/read"
)

// IMAPMover moves IMAP messages between mailboxes.
type IMAPMover interface {
	MoveMessage(ctx context.Context, messageID, fromMailbox, toMailbox string) error
}

// Server is the HTTP web server.
type Server struct {
	st     store.EmailStore
	relay  relay.Sender
	imap   IMAPMover // may be nil if IMAP not configured
	webSrv *http.Server
	apiSrv *http.Server
	t      *template.Template
}

// New creates a new web Server. imapClient may be nil if IMAP is not configured.
func New(st store.EmailStore, r relay.Sender, imapClient IMAPMover) *Server {
	funcMap := template.FuncMap{
		"join": strings.Join,
	}
	t := template.Must(template.New("index.html").Funcs(funcMap).Parse(indexHTML))
	s := &Server{st: st, relay: r, imap: imapClient, t: t}

	webMux := http.NewServeMux()
	webMux.HandleFunc("GET /", s.handleList)
	webMux.HandleFunc("POST /email/{id}/approve", s.handleApprove)
	webMux.HandleFunc("POST /email/{id}/reject", s.handleReject)
	s.webSrv = &http.Server{Handler: webMux}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/emails", s.handleCreateEmail)
	apiMux.HandleFunc("GET /api/emails", s.handleGetEmails)
	s.apiSrv = &http.Server{Handler: apiMux}

	return s
}

// Serve starts the web UI server on addr. Blocks until the server stops.
func (s *Server) Serve(addr string) error {
	s.webSrv.Addr = addr
	log.Printf("Web UI listening on http://%s", addr)
	if err := s.webSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ServeAPI starts the REST API server on addr. Blocks until the server stops.
func (s *Server) ServeAPI(addr string) error {
	s.apiSrv.Addr = addr
	log.Printf("API listening on http://%s", addr)
	if err := s.apiSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops both the web UI and API servers.
func (s *Server) Shutdown(ctx context.Context) error {
	err1 := s.webSrv.Shutdown(ctx)
	err2 := s.apiSrv.Shutdown(ctx)
	if err1 != nil {
		return err1
	}
	return err2
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	emails, err := s.st.ListPending(r.Context())
	if err != nil {
		http.Error(w, "failed to list emails", http.StatusInternalServerError)
		log.Printf("list pending emails: %v", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.t.Execute(w, emails); err != nil {
		log.Printf("render template: %v", err)
	}
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	email, err := s.st.Get(ctx, id)
	if err != nil {
		http.Error(w, "email not found", http.StatusNotFound)
		return
	}

	switch email.Direction {
	case store.DirectionOutbound:
		// Relay via SMTP then delete.
		if err := s.relay.Send(ctx, email); err != nil {
			http.Error(w, "failed to relay email", http.StatusInternalServerError)
			log.Printf("relay email %s: %v", id, err)
			return
		}
		if err := s.st.Delete(ctx, id); err != nil {
			log.Printf("delete email %s after relay: %v", id, err)
		}
	case store.DirectionInbound:
		// Approve in DB and move IMAP message to approved folder.
		if err := s.st.Approve(ctx, id); err != nil {
			http.Error(w, "failed to approve email", http.StatusInternalServerError)
			log.Printf("approve email %s: %v", id, err)
			return
		}
		if s.imap != nil && email.IMAPMessageID != "" {
			if err := s.imap.MoveMessage(ctx, email.IMAPMessageID, folderReceived, folderApproved); err != nil {
				log.Printf("IMAP move email %s to approved: %v", id, err)
			} else if err := s.st.UpdateIMAPMailbox(ctx, id, folderApproved); err != nil {
				log.Printf("update imap mailbox for %s: %v", id, err)
			}
		}
	default:
		http.Error(w, "unknown direction", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	email, err := s.st.Get(ctx, id)
	if err != nil {
		http.Error(w, "email not found", http.StatusNotFound)
		log.Printf("get email %s for reject: %v", id, err)
		return
	}

	if email.Direction == store.DirectionInbound && s.imap != nil && email.IMAPMessageID != "" {
		if err := s.imap.MoveMessage(ctx, email.IMAPMessageID, folderReceived, folderRejected); err != nil {
			log.Printf("IMAP move email %s to rejected: %v", id, err)
		}
	}

	if err := s.st.Delete(ctx, id); err != nil {
		http.Error(w, "email not found", http.StatusNotFound)
		log.Printf("delete email %s: %v", id, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type createEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

type createEmailResponse struct {
	ID string `json:"id"`
}

func (s *Server) handleCreateEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req createEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.From == "" || len(req.To) == 0 || req.Subject == "" {
		http.Error(w, "from, to, and subject are required", http.StatusBadRequest)
		return
	}

	// Build RFC 2822 raw message.
	rawMessage := fmt.Sprintf(
		"Date: %s\r\nMessage-Id: <%s@mailescrow>\r\nFrom: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		time.Now().UTC().Format(time.RFC1123Z),
		uuid.New().String(),
		req.From,
		strings.Join(req.To, ", "),
		req.Subject,
		req.Body,
	)

	id, err := s.st.SaveOutbound(ctx, req.From, req.To, req.Subject, req.Body, []byte(rawMessage))
	if err != nil {
		http.Error(w, "failed to save email", http.StatusInternalServerError)
		log.Printf("save outbound email: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(createEmailResponse{ID: id}); err != nil {
		log.Printf("encode response: %v", err)
	}
}

type emailResponse struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	ReceivedAt time.Time `json:"received_at"`
}

func (s *Server) handleGetEmails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	emails, err := s.st.ListApproved(ctx)
	if err != nil {
		http.Error(w, "failed to list emails", http.StatusInternalServerError)
		log.Printf("list approved emails: %v", err)
		return
	}

	var results []emailResponse
	for _, email := range emails {
		results = append(results, emailResponse{
			ID:         email.ID,
			From:       email.Sender,
			To:         email.Recipients,
			Subject:    email.Subject,
			Body:       email.Body,
			ReceivedAt: email.ReceivedAt,
		})
		// Move to mailescrow/read and delete from DB.
		if s.imap != nil && email.IMAPMessageID != "" {
			if err := s.imap.MoveMessage(ctx, email.IMAPMessageID, folderApproved, folderRead); err != nil {
				log.Printf("IMAP move email %s to read: %v", email.ID, err)
			}
		}
		if err := s.st.Delete(ctx, email.ID); err != nil {
			log.Printf("delete email %s after fetch: %v", email.ID, err)
		}
	}

	if results == nil {
		results = []emailResponse{} // return [] not null
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		log.Printf("encode response: %v", err)
	}
}
