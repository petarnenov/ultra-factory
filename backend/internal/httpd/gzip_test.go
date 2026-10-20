package httpd

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func gzipTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	big := `{"activities":"` + strings.Repeat("a", 8192) + `"}`
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, big)
	})
	mux.HandleFunc("/small", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/text", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, strings.Repeat("a", 8192))
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hi\n\n")
		w.(http.Flusher).Flush()
	})
	mux.HandleFunc("/created", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, big)
	})
	srv := httptest.NewServer(gzipJSON(mux))
	t.Cleanup(srv.Close)
	return srv
}

func gzipGet(t *testing.T, url, acceptEncoding string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	// Setting Accept-Encoding by hand stops the transport decoding for us.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func TestGzipJSONCompressesLargeJSONWhenAccepted(t *testing.T) {
	srv := gzipTestServer(t)
	resp, body := gzipGet(t, srv.URL+"/big", "gzip")
	if resp.Header.Get("Content-Encoding") != "gzip" || resp.Header.Get("Vary") != "Accept-Encoding" {
		t.Fatalf("headers = %v", resp.Header)
	}
	zr, err := gzip.NewReader(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(plain), `{"activities":"aaa`) || len(body) >= len(plain)/10 {
		t.Fatalf("compressed=%d plain=%d", len(body), len(plain))
	}
}

func TestGzipJSONLeavesResponsesAloneOtherwise(t *testing.T) {
	srv := gzipTestServer(t)
	for _, tc := range []struct{ name, path, ae string }{
		{"no accept-encoding", "/big", ""},
		{"gzip refused", "/big", "gzip;q=0"},
		{"below threshold", "/small", "gzip"},
		{"not json", "/text", "gzip"},
		{"sse", "/sse", "gzip"},
	} {
		resp, body := gzipGet(t, srv.URL+tc.path, tc.ae)
		if resp.Header.Get("Content-Encoding") != "" {
			t.Errorf("%s: unexpectedly encoded", tc.name)
		}
		if tc.path == "/sse" && string(body) != "data: hi\n\n" {
			t.Errorf("sse body = %q", body)
		}
		if tc.path == "/big" && len(body) < 8192 {
			t.Errorf("%s: body len %d", tc.name, len(body))
		}
	}
}

func TestGzipJSONTransparentToGoClient(t *testing.T) {
	srv := gzipTestServer(t)
	resp, err := http.Get(srv.URL + "/big")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !resp.Uncompressed || len(body) < 8192 {
		t.Fatalf("uncompressed=%v len=%d", resp.Uncompressed, len(body))
	}
}

func TestGzipJSONKeepsFinalStatusAfterEarlyHints(t *testing.T) {
	srv := gzipTestServer(t)
	resp, _ := gzipGet(t, srv.URL+"/created", "gzip")
	if resp.StatusCode != http.StatusCreated || resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("status=%d enc=%q", resp.StatusCode, resp.Header.Get("Content-Encoding"))
	}
}
