// Package prom is the real EnergySource: it reads accelerator energy from
// Prometheus (which scrapes DCGM-exporter / NPU-Exporter out-of-band), so the
// energy aggregator never touches hardware or the inference path directly. The
// HTTP client mirrors builder/loki's client conventions.
package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client runs Prometheus instant queries and returns the summed numeric result.
type Client interface {
	// Query evaluates an instant query at time at (zero = server "now") and
	// returns the sum of the result's sample values. An empty result is 0.
	Query(ctx context.Context, query string, at time.Time) (float64, error)
}

type client struct {
	url        string
	basicAuth  string // pre-encoded "base64(user:pass)" if set
	httpClient *http.Client
}

// NewClient builds a Prometheus query client for apiAddress (e.g.
// http://prometheus-server). basicAuth, if non-empty, is sent as the
// Authorization: Basic value verbatim.
func NewClient(apiAddress, basicAuth string) Client {
	return &client{
		url:       strings.TrimRight(apiAddress, "/"),
		basicAuth: basicAuth,
		// Queries are infrequent (once per window), so keep-alives give no benefit
		// and a connection left stale by a Prometheus restart/hiccup could otherwise
		// poison the pool; a fresh connection per query keeps the reader robust.
		httpClient: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{DisableKeepAlives: true},
		},
	}
}

type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

func (c *client) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	params := url.Values{}
	params.Set("query", query)
	if !at.IsZero() {
		params.Set("time", strconv.FormatInt(at.Unix(), 10))
	}
	endpoint := fmt.Sprintf("%s/api/v1/query?%s", c.url, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create prometheus query request: %w", err)
	}
	if c.basicAuth != "" {
		req.Header.Set("Authorization", "Basic "+c.basicAuth)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send prometheus query: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus query status %d: %s", resp.StatusCode, string(body))
	}

	var qr queryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return 0, fmt.Errorf("decode prometheus response: %w", err)
	}
	if qr.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s: %s", qr.ErrorType, qr.Error)
	}
	return sumResult(qr.Data.ResultType, qr.Data.Result)
}

// sumResult totals the values of an instant-query result. It handles "vector"
// (sum of all series) and "scalar"; anything else (or empty) is 0.
func sumResult(resultType string, raw json.RawMessage) (float64, error) {
	switch resultType {
	case "vector":
		var samples []struct {
			Value []json.RawMessage `json:"value"` // [<ts number>, "<value string>"]
		}
		if err := json.Unmarshal(raw, &samples); err != nil {
			return 0, fmt.Errorf("decode vector result: %w", err)
		}
		var total float64
		for _, s := range samples {
			if len(s.Value) != 2 {
				continue
			}
			v, err := parsePromValue(s.Value[1])
			if err != nil {
				return 0, err
			}
			total += v
		}
		return total, nil
	case "scalar":
		var val []json.RawMessage
		if err := json.Unmarshal(raw, &val); err != nil {
			return 0, fmt.Errorf("decode scalar result: %w", err)
		}
		if len(val) != 2 {
			return 0, nil
		}
		return parsePromValue(val[1])
	default:
		return 0, nil
	}
}

// parsePromValue parses a Prometheus sample value, which is a JSON string like "123.45".
func parsePromValue(raw json.RawMessage) (float64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("parse prometheus value: %w", err)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse prometheus float %q: %w", s, err)
	}
	return f, nil
}
