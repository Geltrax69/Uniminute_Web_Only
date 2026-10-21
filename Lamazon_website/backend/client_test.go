package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticatedReadsDoNotReuseRevokedSessionCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"session revoked"}`))
			return
		}
		w.Write([]byte(`{"email":"fixture@example.test"}`))
	}))
	defer srv.Close()
	b := NewBackend(srv.URL)
	if _, err := b.Me(context.Background(), "fixture-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Me(context.Background(), "fixture-token"); err == nil {
		t.Fatal("revoked session received cached private data")
	}
	if calls != 2 {
		t.Fatal("authenticated read bypassed live authorization")
	}
}
