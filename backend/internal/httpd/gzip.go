package httpd

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// gzipMinSize is the smallest JSON body worth compressing.
const gzipMinSize = 1024

// gzipJSON gzips large application/json responses for clients that accept it;
// upgrades, SSE and other content types pass through untouched.
func gzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || r.Header.Get("Upgrade") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.finish()
		next.ServeHTTP(gw, r)
	})
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		params = strings.ReplaceAll(strings.ToLower(params), " ", "")
		return params != "q=0" && params != "q=0.0" && params != "q=0.00" && params != "q=0.000"
	}
	return false
}

type gzipMode int

const (
	gzipUndecided gzipMode = iota // buffering until the body size is known
	gzipPassthrough
	gzipCompress
)

type gzipWriter struct {
	http.ResponseWriter
	mode   gzipMode
	status int
	buf    []byte
	zw     *gzip.Writer
}

func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) WriteHeader(status int) {
	if g.mode != gzipUndecided || g.status != 0 {
		return
	}
	g.status = status
	if !g.eligible(status) {
		g.passthrough()
	}
}

func (g *gzipWriter) eligible(status int) bool {
	h := g.Header()
	if status < 200 || status == http.StatusNoContent || status == http.StatusPartialContent || status == http.StatusNotModified {
		return false
	}
	if h.Get("Content-Encoding") != "" || h.Get("Content-Range") != "" {
		return false
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	return strings.HasPrefix(ct, "application/json")
}

func (g *gzipWriter) passthrough() {
	g.mode = gzipPassthrough
	if g.status == 0 {
		g.status = http.StatusOK
	}
	g.ResponseWriter.WriteHeader(g.status)
	if len(g.buf) > 0 {
		_, _ = g.ResponseWriter.Write(g.buf)
		g.buf = nil
	}
}

func (g *gzipWriter) startCompress() {
	g.mode = gzipCompress
	h := g.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	g.ResponseWriter.WriteHeader(g.status)
	g.zw = gzip.NewWriter(g.ResponseWriter)
	_, _ = g.zw.Write(g.buf)
	g.buf = nil
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if g.mode == gzipUndecided && g.status == 0 {
		g.WriteHeader(http.StatusOK)
	}
	switch g.mode {
	case gzipPassthrough:
		return g.ResponseWriter.Write(p)
	case gzipCompress:
		return g.zw.Write(p)
	}
	g.buf = append(g.buf, p...)
	if len(g.buf) >= gzipMinSize {
		g.startCompress()
	}
	return len(p), nil
}

// Flush means the handler is streaming, so send any buffered prefix plain.
func (g *gzipWriter) Flush() {
	if g.mode == gzipUndecided {
		if g.status == 0 {
			g.status = http.StatusOK
		}
		g.passthrough()
	}
	if g.mode == gzipCompress {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipWriter) finish() {
	switch g.mode {
	case gzipUndecided:
		if g.status != 0 {
			g.passthrough()
		}
	case gzipCompress:
		_ = g.zw.Close()
	}
}
