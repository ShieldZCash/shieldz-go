package shieldz

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/url"
)

// Invoices creates and reads invoices. An invoice's pay_url is the hosted checkout.
type Invoices struct{ c *Client }

// Create makes an invoice, e.g. Params{"amount_usd_cents": 5000, "memo": "Order #1234"}.
func (s *Invoices) Create(ctx context.Context, p Params) (Object, error) {
	var o Object
	return o, s.c.Do(ctx, http.MethodPost, "/invoices", nil, p, &o)
}

// Retrieve fetches one invoice by id.
func (s *Invoices) Retrieve(ctx context.Context, id string) (Object, error) {
	var o Object
	return o, s.c.Do(ctx, http.MethodGet, "/invoices/"+url.PathEscape(id), nil, nil, &o)
}

// ListParams filters an invoice list. Zero values are omitted.
type ListParams struct {
	Limit         int
	Status        string
	StartingAfter string
}

func (p ListParams) values() url.Values {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", fmt.Sprint(p.Limit))
	}
	if p.Status != "" {
		q.Set("status", p.Status)
	}
	if p.StartingAfter != "" {
		q.Set("starting_after", p.StartingAfter)
	}
	return q
}

// List returns one page of invoices, newest first.
func (s *Invoices) List(ctx context.Context, p ListParams) (*List, error) {
	var l List
	return &l, s.c.Do(ctx, http.MethodGet, "/invoices", p.values(), nil, &l)
}

// All iterates every invoice, following the cursor across pages.
//
//	for inv, err := range client.Invoices.All(ctx, shieldz.ListParams{Status: "paid"}) { ... }
func (s *Invoices) All(ctx context.Context, p ListParams) iter.Seq2[Object, error] {
	return func(yield func(Object, error) bool) {
		for {
			page, err := s.List(ctx, p)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, inv := range page.Data {
				if !yield(inv, nil) {
					return
				}
			}
			if !page.HasMore || len(page.Data) == 0 {
				return
			}
			p.StartingAfter = page.Data[len(page.Data)-1].String("id")
		}
	}
}

// Catalog covers products, prices and payment links: the catalog a buy button
// or payment link sells from, so the buyer's browser never sets the price.
type Catalog struct {
	c    *Client
	path string
}

// Create adds a product, price or payment link.
func (s *Catalog) Create(ctx context.Context, p Params) (Object, error) {
	var o Object
	return o, s.c.Do(ctx, http.MethodPost, s.path, nil, p, &o)
}

// Retrieve fetches one object by id.
func (s *Catalog) Retrieve(ctx context.Context, id string) (Object, error) {
	var o Object
	return o, s.c.Do(ctx, http.MethodGet, s.path+"/"+url.PathEscape(id), nil, nil, &o)
}

// Update changes an object. Products take default_price_data to add a price; prices only active.
func (s *Catalog) Update(ctx context.Context, id string, p Params) (Object, error) {
	var o Object
	return o, s.c.Do(ctx, http.MethodPost, s.path+"/"+url.PathEscape(id), nil, p, &o)
}

// List returns a page. Products filter on "active", prices on "product".
func (s *Catalog) List(ctx context.Context, q url.Values) (*List, error) {
	var l List
	return &l, s.c.Do(ctx, http.MethodGet, s.path, q, nil, &l)
}
