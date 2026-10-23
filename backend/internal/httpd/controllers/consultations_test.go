package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/consultations"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
)

type consultationFake struct {
	err    error
	limit  int
	cursor string
	id     string
}

func (f *consultationFake) List(_ context.Context, n int, cursor string) (consultations.Page, error) {
	f.limit = n
	f.cursor = cursor
	return consultations.Page{Items: []consultations.Summary{}}, f.err
}
func (f *consultationFake) Detail(_ context.Context, id string) (consultations.Detail, error) {
	f.id = id
	return consultations.Detail{Summary: consultations.Summary{ID: id}}, f.err
}
func TestConsultationRoutes(t *testing.T) {
	for _, path := range []string{"/knowledge/consultations", "/knowledge/consultations/id"} {
		srv := newRouterServer(t, httpd.APIDeps{})
		resp, _ := doKnowledge(t, "GET", srv.URL+"/api/v1"+path, "")
		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatal(resp.StatusCode)
		}
	}
	for _, test := range []struct {
		path   string
		err    error
		status int
	}{
		{"", nil, 200}, {"?limit=50&cursor=opaque", nil, 200},
		{"?limit=0", nil, 400}, {"?limit=51", nil, 400}, {"?limit=no", nil, 400},
		{"?cursor=bad", consultations.ErrInvalid, 400}, {"", errors.New("secret path"), 503},
		{"/opaque", nil, 200}, {"/opaque", consultations.ErrMissing, 404}, {"/opaque", consultations.ErrInvalid, 400}, {"/opaque", errors.New("secret path"), 503},
	} {
		t.Run(test.path+http.StatusText(test.status), func(t *testing.T) {
			fake := &consultationFake{err: test.err}
			srv := newRouterServer(t, httpd.APIDeps{Consultations: fake})
			resp, raw := doKnowledge(t, "GET", srv.URL+"/api/v1/knowledge/consultations"+test.path, "")
			if resp.StatusCode != test.status {
				t.Fatalf("%d %s", resp.StatusCode, raw)
			}
			if strings.Contains(string(raw), "secret path") {
				t.Fatal("leaked graph error")
			}
			if test.status >= 400 && (!strings.Contains(string(raw), `"code"`) || !strings.Contains(string(raw), `"requestId":"`)) {
				t.Fatalf("missing error envelope %s", raw)
			}
			if test.path == "?limit=50&cursor=opaque" && (fake.limit != 50 || fake.cursor != "opaque") {
				t.Fatal(fake)
			}
		})
	}
}
