package telemetrymeta

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
)

func TestErrorKindAndCode(t *testing.T) {
	kind, code := ErrorKindAndCode(apierr.NotFound("SESSION_NOT_FOUND", "Unknown session"))
	if kind != "not_found" || code != "SESSION_NOT_FOUND" {
		t.Fatalf("typed error = (%q, %q), want (not_found, SESSION_NOT_FOUND)", kind, code)
	}

	kind, code = ErrorKindAndCode(errors.New("boom"))
	if kind != "internal" || code != "" {
		t.Fatalf("raw error = (%q, %q), want (internal, empty)", kind, code)
	}
}

func TestRoutePatternPrefersChiPattern(t *testing.T) {
	var got string
	r := chi.NewRouter()
	r.Get("/api/v1/projects/{projectID}/sessions/{sessionID}", func(w http.ResponseWriter, req *http.Request) {
		got = RoutePattern(req)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/mer/sessions/sess-1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got != "/api/v1/projects/{projectID}/sessions/{sessionID}" {
		t.Fatalf("route pattern = %q, want chi route pattern", got)
	}
}

func TestFingerprintStableForSameInputs(t *testing.T) {
	first := Fingerprint("httpd", "http_request", "GET", "/api/v1/projects/{projectID}", "5xx", "internal")
	second := Fingerprint("httpd", "http_request", "GET", "/api/v1/projects/{projectID}", "5xx", "internal")
	other := Fingerprint("httpd", "http_request", "POST", "/api/v1/projects/{projectID}", "5xx", "internal")

	if first == "" || len(first) != 16 {
		t.Fatalf("fingerprint = %q, want 16-char digest", first)
	}
	if first != second {
		t.Fatalf("fingerprints differ for same inputs: %q vs %q", first, second)
	}
	if first == other {
		t.Fatalf("fingerprints should differ for different inputs: %q vs %q", first, other)
	}
}

func TestErrorKindMapsEveryKind(t *testing.T) {
	cases := []struct {
		kind apierr.Kind
		want string
	}{
		{apierr.KindInvalid, "invalid"},
		{apierr.KindNotFound, "not_found"},
		{apierr.KindConflict, "conflict"},
		{apierr.KindForbidden, "forbidden"},
		{apierr.KindTooManyRequests, "too_many_requests"},
		{apierr.KindNotImplemented, "not_implemented"},
		{apierr.KindUnavailable, "unavailable"},
		{apierr.KindInternal, "internal"},
		{apierr.Kind(999), "internal"},
	}
	for _, tc := range cases {
		if got := ErrorKind(tc.kind); got != tc.want {
			t.Errorf("ErrorKind(%v) = %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestErrorKindAndCodeUnwrapsKnownAndUnknown(t *testing.T) {
	kind, code := ErrorKindAndCode(apierr.Invalid("BAD_INPUT", "bad input", nil))
	if kind != "invalid" || code != "BAD_INPUT" {
		t.Fatalf("invalid typed error = (%q, %q), want (invalid, BAD_INPUT)", kind, code)
	}

	kind, code = ErrorKindAndCode(apierr.Internal("INTERNAL", "boom"))
	if kind != "internal" || code != "INTERNAL" {
		t.Fatalf("internal typed error = (%q, %q), want (internal, INTERNAL)", kind, code)
	}

	kind, code = ErrorKindAndCode(nil)
	if kind != "internal" || code != "" {
		t.Fatalf("nil error = (%q, %q), want (internal, empty)", kind, code)
	}
}

func TestPanicKindClassifiesPayloads(t *testing.T) {
	cases := []struct {
		panicValue any
		want       string
	}{
		{errors.New("boom"), "error"},
		{"boom", "string"},
		{42, "other"},
		{nil, "other"},
	}
	for _, tc := range cases {
		if got := PanicKind(tc.panicValue); got != tc.want {
			t.Errorf("PanicKind(%v) = %q, want %q", tc.panicValue, got, tc.want)
		}
	}
}

func TestStatusFamilyClasses(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{99, "unknown"},
		{100, "1xx"},
		{199, "1xx"},
		{200, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{429, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{999, "9xx"},
		{1000, "unknown"},
	}
	for _, tc := range cases {
		if got := StatusFamily(tc.status); got != tc.want {
			t.Errorf("StatusFamily(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestFingerprintSkipsEmptyParts(t *testing.T) {
	withBlanks := Fingerprint("", "  ", "httpd", "", "5xx")
	withoutBlanks := Fingerprint("httpd", "5xx")
	if withBlanks != withoutBlanks {
		t.Fatalf("empty parts changed fingerprint: %q vs %q", withBlanks, withoutBlanks)
	}
	if withBlanks == "" || len(withBlanks) != 16 {
		t.Fatalf("fingerprint = %q, want 16-char digest", withBlanks)
	}
}

func TestRoutePatternFallbacks(t *testing.T) {
	if got := RoutePattern(nil); got != "" {
		t.Fatalf("RoutePattern(nil) = %q, want empty", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/plain/path", nil)
	if got := RoutePattern(req); got != "/plain/path" {
		t.Fatalf("RoutePattern(no route context) = %q, want /plain/path", got)
	}

	req.URL = nil
	if got := RoutePattern(req); got != "" {
		t.Fatalf("RoutePattern(nil URL) = %q, want empty", got)
	}
}
