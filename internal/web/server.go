package web

import (
	"context"
	_ "embed"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/albert/mailescrow/internal/relay"
	"github.com/albert/mailescrow/internal/store"
)

//go:embed templates/index.html
var indexHTML string

// Server is the HTTP web server.
type Server struct {
	st      store.EmailStore
	relay   relay.Sender
	httpSrv *http.Server
	t       *template.Template
}

// New creates a new web Server.
func New(st store.EmailStore, r relay.Sender) *Server {
	funcMap := template.FuncMap{
		"join": strings.Join,
	}
	t := template.Must(template.New("index.html").Funcs(funcMap).Parse(indexHTML))
	s := &Server{st: st, relay: r, t: t}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleList)
	mux.HandleFunc("POST /email/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /email/{id}/reject", s.handleReject)

	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// Serve starts listening on the given address. Blocks until the server stops.
func (s *Server) Serve(addr string) error {
	s.httpSrv.Addr = addr
	log.Printf("Web UI listening on http://%s", addr)
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	emails, err := s.st.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list emails", http.StatusInternalServerError)
		log.Printf("list emails: %v", err)
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
	if err := s.relay.Send(ctx, email); err != nil {
		http.Error(w, "failed to relay email", http.StatusInternalServerError)
		log.Printf("relay email %s: %v", id, err)
		return
	}
	if err := s.st.Delete(ctx, id); err != nil {
		log.Printf("delete email %s after relay: %v", id, err)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if err := s.st.Delete(ctx, id); err != nil {
		http.Error(w, "email not found", http.StatusNotFound)
		log.Printf("delete email %s: %v", id, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
