package remotehost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAndGetJSONCarriesCredentialAfterIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/identity":
			_, _ = w.Write([]byte(`{"hostId":"h_remote","apiVersion":1}`))
		case "/api/v1/projects":
			if r.Header.Get("Authorization") != "Bearer pw" || r.Header.Get("X-AO-Expected-Host-ID") != "h_remote" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"projects":[{"id":"app"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	entry := Entry{HostID: "h_remote", URL: srv.URL, Password: "pw"}
	session, err := Open(context.Background(), nil, entry, 5*time.Second)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer session.Close()
	var list struct {
		Projects []struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	if err := session.GetJSON(context.Background(), "/api/v1/projects", &list); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if len(list.Projects) != 1 || list.Projects[0].ID != "app" {
		t.Fatalf("projects = %+v", list.Projects)
	}
}

func TestOpenRefusesIdentityMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hostId":"h_other","apiVersion":1}`))
	}))
	t.Cleanup(srv.Close)
	_, err := Open(context.Background(), nil, Entry{HostID: "h_remote", URL: srv.URL, Password: "pw"}, 5*time.Second)
	if err == nil {
		t.Fatal("Open accepted a different host identity")
	}
}

func TestGetJSONMapsRefusedCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/identity" {
			_, _ = w.Write([]byte(`{"hostId":"h_remote","apiVersion":1}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	session, err := Open(context.Background(), nil, Entry{HostID: "h_remote", URL: srv.URL, Password: "bad"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer session.Close()
	var out struct{}
	if err := session.GetJSON(context.Background(), "/api/v1/projects", &out); !errors.Is(err, ErrCredentialRefused) {
		t.Fatalf("GetJSON error = %v, want ErrCredentialRefused", err)
	}
}
