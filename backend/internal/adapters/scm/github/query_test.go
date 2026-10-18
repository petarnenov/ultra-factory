package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryDecodesDataAndClassifiesErrors(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		if gotBody["query"] == "bad" {
			_, _ = w.Write([]byte(`{"errors":[{"message":"Bad credentials"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"name":"r","stars":3}}}`))
	}))
	defer srv.Close()
	c := NewClient(ClientOptions{Token: StaticTokenSource("tok"), GraphQLURL: srv.URL})

	var out struct {
		Repository struct {
			Name  string `json:"name"`
			Stars int    `json:"stars"`
		} `json:"repository"`
	}
	if err := c.Query(context.Background(), "q", map[string]any{"owner": "o"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Repository.Name != "r" || out.Repository.Stars != 3 {
		t.Fatalf("decoded %+v", out)
	}
	if gotAuth != "Bearer tok" && gotAuth != "token tok" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if vars, _ := gotBody["variables"].(map[string]any); vars["owner"] != "o" {
		t.Fatalf("variables not sent: %+v", gotBody)
	}
	if err := c.Query(context.Background(), "bad", nil, &out); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
}
