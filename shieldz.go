// Package shieldz is the Go client for the Shieldz API: non-custodial crypto
// payments with $0 platform fees. Payments settle straight to the merchant's
// own wallet in USDC/USDT (Base, Arbitrum, Optimism, Polygon, Ethereum),
// Bitcoin or Zcash.
//
//	client := shieldz.New(os.Getenv("SHIELDZ_API_KEY"))
//	inv, err := client.Invoices.Create(ctx, shieldz.Params{"amount_usd_cents": 5000, "memo": "Order #1234"})
//
// See https://shieldz.cash/docs for the full API.
package shieldz

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version sent in the User-Agent header.
const Version = "0.1.0"

// DefaultBaseURL is the production API root.
const DefaultBaseURL = "https://shieldz.cash/api/v1"

// Params is a request body or query. Field names match the REST API.
type Params map[string]any

// Object is a decoded API object (invoice, product, price, payment link).
type Object map[string]any

// String returns a string field, or "" when absent.
func (o Object) String(key string) string {
	s, _ := o[key].(string)
	return s
}

// List is one page of a list endpoint.
type List struct {
	Data    []Object `json:"data"`
	HasMore bool     `json:"has_more"`
}

// Client talks to the Shieldz API. Create one with New and reuse it.
type Client struct {
	APIKey        string
	BaseURL       string
	HTTPClient    *http.Client
	MaxRetries    int
	MaxRetryDelay time.Duration

	Invoices     *Invoices
	Products     *Catalog
	Prices       *Catalog
	PaymentLinks *Catalog
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another API root (e.g. a local server).
func WithBaseURL(u string) Option { return func(c *Client) { c.BaseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the default *http.Client (30s timeout).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.HTTPClient = h } }

// WithMaxRetries sets how many times 429/5xx and network errors are retried (default 2).
func WithMaxRetries(n int) Option { return func(c *Client) { c.MaxRetries = max(0, n) } }

// New returns a client authenticated with a secret API key (sk_live_... or sk_test_...).
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		APIKey:        apiKey,
		BaseURL:       DefaultBaseURL,
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		MaxRetries:    2,
		MaxRetryDelay: 8 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	c.Invoices = &Invoices{c}
	c.Products = &Catalog{c, "/products"}
	c.Prices = &Catalog{c, "/prices"}
	c.PaymentLinks = &Catalog{c, "/payment_links"}
	return c
}

// Error is returned for any non-2xx response. Status is 0 for network errors.
type Error struct {
	Status    int
	Type      string
	Code      string
	Message   string
	Param     string
	RequestID string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return "shieldz: " + e.Message
	}
	return fmt.Sprintf("shieldz: API error (HTTP %d)", e.Status)
}

func retryable(status int) bool {
	return status == 429 || status == 500 || status == 502 || status == 503 || status == 504
}

func (c *Client) backoff(attempt int) time.Duration {
	d := min(c.MaxRetryDelay, time.Duration(500*(1<<attempt))*time.Millisecond)
	return time.Duration(float64(d) * (0.5 + mrand.Float64()*0.5))
}

func autoIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "auto_" + hex.EncodeToString(b)
}

// Do sends a request and decodes the JSON response into out (which may be nil).
// Retryable POSTs get an automatic idempotency_key so a retry can't create a duplicate.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body Params, out any) error {
	if c.APIKey == "" {
		return errors.New("shieldz: API key is required")
	}
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	if method == http.MethodPost && c.MaxRetries > 0 && body != nil {
		if _, ok := body["idempotency_key"]; !ok {
			b := make(Params, len(body)+1)
			for k, v := range body {
				b[k] = v
			}
			b["idempotency_key"] = autoIdempotencyKey()
			body = b
		}
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "shieldz-go/"+Version)

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() == nil && attempt < c.MaxRetries {
				if werr := sleep(ctx, c.backoff(attempt)); werr != nil {
					return werr
				}
				continue
			}
			return &Error{Status: 0, Type: "connection_error", Code: "network_error", Message: err.Error()}
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(data) == 0 {
				return nil
			}
			return json.Unmarshal(data, out)
		}
		if retryable(resp.StatusCode) && attempt < c.MaxRetries {
			wait := c.backoff(attempt)
			if s, perr := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); perr == nil {
				wait = time.Duration(s * float64(time.Second))
			}
			if werr := sleep(ctx, wait); werr != nil {
				return werr
			}
			continue
		}
		var env struct {
			Error struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
				Param   string `json:"param"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &env)
		rid := resp.Header.Get("X-Request-Id")
		if rid == "" {
			rid = resp.Header.Get("Cf-Ray")
		}
		e := &Error{Status: resp.StatusCode, Type: env.Error.Type, Code: env.Error.Code,
			Message: env.Error.Message, Param: env.Error.Param, RequestID: rid}
		if e.Type == "" {
			e.Type = "api_error"
		}
		return e
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
