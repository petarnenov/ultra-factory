package remotereports

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type trackedResponseBody struct {
	*strings.Reader
	closed           bool
	remainingOnClose int
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	b.remainingOnClose = b.Len()
	return nil
}

func TestHostClientDrainsResponsesBeforeClosingWithinLimit(t *testing.T) {
	for _, tc := range []struct {
		name          string
		payload       string
		decode        bool
		wantRemaining int
	}{
		{name: "acknowledgement", payload: `{"acknowledged":[]}`},
		{name: "decoded response with trailing whitespace", payload: `{}` + strings.Repeat(" ", 1024), decode: true},
		{name: "oversized response", payload: strings.Repeat("x", maxResponseBytes+1), wantRemaining: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedResponseBody{Reader: strings.NewReader(tc.payload)}
			client := hostClient{
				base: "http://remote.test",
				http: http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
				})},
			}
			var out any
			if tc.decode {
				out = &struct{}{}
			}
			if err := client.do(context.Background(), http.MethodPost, "/api/v1/reports/acknowledge", nil, out); err != nil {
				t.Fatal(err)
			}
			if !body.closed || body.remainingOnClose != tc.wantRemaining {
				t.Fatalf("response closed=%v with %d bytes remaining, want closed with %d", body.closed, body.remainingOnClose, tc.wantRemaining)
			}
		})
	}
}
