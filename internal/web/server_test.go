package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuthMiddleware(t *testing.T) {
	s := &Server{password: "secret"}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := s.basicAuth(inner)

	t.Run("no credentials returns 401", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
		if called {
			t.Error("inner handler should not have been called")
		}
		if w.Header().Get("WWW-Authenticate") == "" {
			t.Error("WWW-Authenticate header missing")
		}
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("anyuser", "wrong")
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
		if called {
			t.Error("inner handler should not have been called")
		}
	})

	t.Run("correct password passes through", func(t *testing.T) {
		called = false
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("anyuser", "secret")
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if !called {
			t.Error("inner handler should have been called")
		}
	})

	t.Run("no password configured skips auth", func(t *testing.T) {
		called = false
		noAuth := &Server{password: ""}
		h := noAuth.basicAuth(inner)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if !called {
			t.Error("inner handler should have been called when no password is set")
		}
	})
}
