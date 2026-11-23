# shieldz-go

[![Go Reference](https://pkg.go.dev/badge/github.com/ShieldZCash/shieldz-go.svg)](https://pkg.go.dev/github.com/ShieldZCash/shieldz-go)

Go client for [Shieldz](https://shieldz.cash): non-custodial crypto payments with $0 platform fees. Accept USDC and USDT on Base, Arbitrum, Optimism, Polygon and Ethereum, plus native Bitcoin and Zcash. Payments settle straight to your own wallet; Shieldz never holds funds or keys.

- No dependencies, standard library only.
- Retries 429/5xx and network errors with backoff; retried creates reuse one idempotency key, so they can't duplicate.
- Cursor pagination as a Go 1.23 iterator.
- Webhook signature verification.

## Install

```
go get github.com/ShieldZCash/shieldz-go
```

## Create an invoice

```go
client := shieldz.New(os.Getenv("SHIELDZ_API_KEY"))

inv, err := client.Invoices.Create(ctx, shieldz.Params{
	"amount_usd_cents": 5000, // $50.00
	"memo":             "Order #1234",
	"metadata":         map[string]string{"order_id": "1234"},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(inv.String("pay_url")) // send the customer to the hosted checkout
```

## List invoices

```go
for inv, err := range client.Invoices.All(ctx, shieldz.ListParams{Status: "paid"}) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(inv.String("id"))
}
```

## Products, prices and payment links

```go
product, err := client.Products.Create(ctx, shieldz.Params{
	"name":               "Pro plan",
	"default_price_data": map[string]any{"unit_amount_usd_cents": 2900},
})
```

`client.Prices` and `client.PaymentLinks` work the same way.

## Webhooks

```go
func handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	event, err := shieldz.ConstructEvent(body, r.Header.Get("X-Shieldz-Signature"), os.Getenv("SHIELDZ_WEBHOOK_SECRET"))
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	if event.String("type") == "invoice.paid" {
		// fulfil the order
	}
	w.WriteHeader(http.StatusOK)
}
```

## Errors

Every non-2xx response is a `*shieldz.Error` with `Status`, `Type`, `Code`, `Message`, `Param` and `RequestID`.

## Links

- API docs: https://shieldz.cash/docs
- OpenAPI: https://shieldz.cash/openapi.json
- Other SDKs: [TypeScript](https://github.com/ShieldZCash/shieldz-sdk), [Python](https://github.com/ShieldZCash/shieldz-python), [Rust](https://github.com/ShieldZCash/shieldz-rust), [PHP](https://github.com/ShieldZCash/shieldz-php)

MIT licensed.
