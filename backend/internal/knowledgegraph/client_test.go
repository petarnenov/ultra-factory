package knowledgegraph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestClientRunSendsAuthenticatedQuery(t *testing.T) {
	var gotPath, gotUser, gotPass, gotType string
	var gotBody queryRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		gotType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":{"fields":["id","n"],"values":[["a",1],["b"]]}}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, User: "ao", Password: "pw", Database: "graph"}, nil)

	rows, err := c.Run(context.Background(), "RETURN 1", map[string]any{"x": "y"})

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotPath != "/db/graph/query/v2" || gotUser != "ao" || gotPass != "pw" || gotType != "application/json" {
		t.Fatalf("request path=%q user=%q pass=%q type=%q", gotPath, gotUser, gotPass, gotType)
	}
	if gotBody.Statement != "RETURN 1" || gotBody.Parameters["x"] != "y" {
		t.Fatalf("body = %+v", gotBody)
	}
	want := []Row{{"id": "a", "n": float64(1)}, {"id": "b"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestClientRunErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "errors array", status: http.StatusOK, body: `{"errors":[{"code":"Neo.ClientError.Syntax","message":"bad"}]}`, wantErr: "Neo.ClientError.Syntax: bad"},
		{name: "errors array wins over status", status: http.StatusBadRequest, body: `{"errors":[{"code":"C","message":"m"}]}`, wantErr: "C: m"},
		{name: "non-2xx without body", status: http.StatusServiceUnavailable, wantErr: "HTTP 503"},
		{name: "undecodable body", status: http.StatusBadGateway, body: `<html>`, wantErr: "HTTP 502: undecodable response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			c := NewClient(Config{URL: srv.URL, Database: "neo4j"}, srv.Client())

			_, err := c.Run(context.Background(), "RETURN 1", nil)

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Run() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestClientRunTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := NewClient(Config{URL: url, Database: "neo4j"}, nil)

	_, err := c.Run(context.Background(), "RETURN 1", nil)

	if err == nil || !strings.HasPrefix(err.Error(), "knowledge graph:") {
		t.Fatalf("Run() error = %v, want knowledge graph transport error", err)
	}
}

func TestRowHelpers(t *testing.T) {
	r := Row{"s": "text", "n": float64(2.5), "list": []any{"a", 1, "", "b"}, "bad": 7}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "string column", got: r.String("s"), want: "text"},
		{name: "string wrong type", got: r.String("n"), want: ""},
		{name: "string missing", got: r.String("absent"), want: ""},
		{name: "float column", got: r.Float("n"), want: 2.5},
		{name: "float wrong type", got: r.Float("s"), want: float64(0)},
		{name: "strings skip non-strings and empty", got: r.Strings("list"), want: []string{"a", "b"}},
		{name: "strings wrong type", got: r.Strings("bad"), want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("got %#v, want %#v", tt.got, tt.want)
			}
		})
	}
}
