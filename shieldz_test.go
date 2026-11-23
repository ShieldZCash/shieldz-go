package shieldz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func sign(secret, t, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(t + "." + body))
	return hex.EncodeToString(m.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := `{"type":"invoice.paid","data":{"id":"inv_1"}}`
	good := sign("whsec_x", ts, body)

	cases := []struct {
		name   string
		header string
		want   error
	}{
		{"valid", "t=" + ts + ",v1=" + good, nil},
		{"rotation", "t=" + ts + ",v1=deadbeef,v1=" + good, nil},
		{"wrong sig", "t=" + ts + ",v1=deadbeef", ErrNoMatchingSig},
		{"missing", "", ErrMissingSignature},
		{"malformed", "v1=" + good, ErrMalformedSignature},
		{"stale", "t=" + strconv.FormatInt(now.Unix()-301, 10) + ",v1=" + good, ErrTimestampTolerance},
	}
	for _, c := range cases {
		err := VerifySignature([]byte(body), c.header, "whsec_x", DefaultTolerance, now)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

func TestCreateRetriesWithSameIdempotencyKey(t *testing.T) {
	var calls atomic.Int32
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test_1" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		keys = append(keys, body["idempotency_key"].(string))
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "inv_1", "status": "pending", "pay_url": "https://shieldz.cash/pay/inv_1"})
	}))
	defer srv.Close()

	c := New("sk_test_1", WithBaseURL(srv.URL))
	inv, err := c.Invoices.Create(context.Background(), Params{"amount_usd_cents": 5000})
	if err != nil {
		t.Fatal(err)
	}
	if inv.String("id") != "inv_1" || calls.Load() != 2 {
		t.Fatalf("got %v after %d calls", inv, calls.Load())
	}
	if keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("idempotency keys differ across retry: %v", keys)
	}
}

func TestErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_9")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"amount_too_small","message":"amount below $1","param":"amount_usd_cents"}}`))
	}))
	defer srv.Close()

	_, err := New("sk_test_1", WithBaseURL(srv.URL)).Invoices.Create(context.Background(), Params{"amount_usd_cents": 1})
	var e *Error
	if !errors.As(err, &e) || e.Status != 400 || e.Code != "amount_too_small" || e.Param != "amount_usd_cents" || e.RequestID != "req_9" {
		t.Fatalf("unexpected error %#v", err)
	}
}

func TestAllFollowsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"}],"has_more":true}`))
			return
		}
		if r.URL.Query().Get("starting_after") != "b" {
			t.Errorf("cursor %q", r.URL.Query().Get("starting_after"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"c"}],"has_more":false}`))
	}))
	defer srv.Close()

	var ids []string
	for inv, err := range New("sk_test_1", WithBaseURL(srv.URL)).Invoices.All(context.Background(), ListParams{Status: "paid"}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, inv.String("id"))
	}
	if len(ids) != 3 || ids[2] != "c" {
		t.Fatalf("ids %v", ids)
	}
}
