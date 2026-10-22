package knowledgegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client runs Cypher through Neo4j's HTTP Query API, so the daemon needs no
// Bolt driver.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a client for cfg. hc nil uses a client bounded by
// cfg.Timeout.
func NewClient(cfg Config, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: hc}
}

// Row is one result row keyed by column name.
type Row map[string]any

type queryRequest struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type queryResponse struct {
	Data struct {
		Fields []string `json:"fields"`
		Values [][]any  `json:"values"`
	} `json:"data"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Run executes one statement in its own transaction.
func (c *Client) Run(ctx context.Context, statement string, params map[string]any) ([]Row, error) {
	body, err := json.Marshal(queryRequest{Statement: statement, Parameters: params})
	if err != nil {
		return nil, err
	}
	endpoint := c.cfg.URL + "/db/" + url.PathEscape(c.cfg.Database) + "/query/v2"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("knowledge graph: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("knowledge graph: read response: %w", err)
	}
	var out queryResponse
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("knowledge graph: HTTP %d: undecodable response", resp.StatusCode)
		}
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("knowledge graph: %s: %s", out.Errors[0].Code, out.Errors[0].Message)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("knowledge graph: HTTP %d", resp.StatusCode)
	}
	rows := make([]Row, 0, len(out.Data.Values))
	for _, values := range out.Data.Values {
		row := make(Row, len(out.Data.Fields))
		for i, field := range out.Data.Fields {
			if i < len(values) {
				row[field] = values[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// String returns a string column or "".
func (r Row) String(key string) string {
	s, _ := r[key].(string)
	return s
}

// Float returns a numeric column or 0.
func (r Row) Float(key string) float64 {
	f, _ := r[key].(float64)
	return f
}

// Strings returns a list-of-strings column, skipping non-strings.
func (r Row) Strings(key string) []string {
	list, _ := r[key].([]any)
	out := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
