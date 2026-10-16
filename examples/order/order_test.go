package order

import (
	"testing"
)

func TestPlaceOrder_success(t *testing.T) {
	audit := &recordingAuditor{}
	svc := &Service{
		Customers: staticCustomers{Customer{ID: "c1", Tier: "gold", CreditLimit: 50_000, BalanceCents: 0}},
		Inventory: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 10}},
		Discounts: staticDiscounts{percent: 10},
		Credit:    allowCredit{},
		Orders:    &recordingStore{},
		Audit:     audit,
	}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	if audit.calls != 1 {
		t.Fatalf("audit calls = %d, want 1", audit.calls)
	}
}

func TestPlaceOrder_invalidQuantity_shortCircuitsInventory(t *testing.T) {
	inv := &countingInventory{
		inner: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 10}},
	}
	svc := &Service{
		Customers: staticCustomers{Customer{ID: "c1", CreditLimit: 50_000}},
		Inventory: inv,
		Discounts: staticDiscounts{percent: 0},
		Credit:    allowCredit{},
		Orders:    &recordingStore{},
	}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "0"})
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400; body %q", resp.StatusCode, resp.Body)
	}
	if resp.Body != "quantity: must be positive" {
		t.Fatalf("body = %q", resp.Body)
	}
	if inv.priceCalls != 0 || inv.stockCalls != 0 {
		t.Fatalf("inventory touched: price=%d stock=%d", inv.priceCalls, inv.stockCalls)
	}
}

func TestPlaceOrder_outOfStock_shortCircuitsPersist(t *testing.T) {
	store := &recordingStore{}
	svc := &Service{
		Customers: staticCustomers{Customer{ID: "c1", CreditLimit: 50_000}},
		Inventory: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 1}},
		Discounts: staticDiscounts{percent: 0},
		Credit:    allowCredit{},
		Orders:    store,
	}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "5"})
	if resp.StatusCode != 409 {
		t.Fatalf("status = %d, want 409; body %q", resp.StatusCode, resp.Body)
	}
	if store.saves != 0 {
		t.Fatalf("persist ran %d times, want 0", store.saves)
	}
}

func TestPlaceOrder_discountUnavailable_recoversWithOrElse(t *testing.T) {
	svc := &Service{
		Customers: staticCustomers{Customer{ID: "c1", CreditLimit: 50_000}},
		Inventory: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 5}},
		Discounts: failingDiscounts{err: ErrDiscountUnavailable},
		Credit:    allowCredit{},
		Orders:    &recordingStore{},
	}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	if resp.Body != "order ord-c1-sku-widget placed total=2000" {
		t.Fatalf("body = %q", resp.Body)
	}
}

// --- test doubles ---

type staticCustomers struct {
	c Customer
}

func (s staticCustomers) FindByID(id string) (Customer, error) {
	if s.c.ID != id {
		return Customer{}, &NotFoundError{Entity: "customer", ID: id}
	}
	return s.c, nil
}

type staticInventory struct {
	prices map[string]int
	stock  map[string]int
}

func (s staticInventory) UnitPriceCents(sku string) (int, error) {
	p, ok := s.prices[sku]
	if !ok {
		return 0, &NotFoundError{Entity: "sku", ID: sku}
	}
	return p, nil
}

func (s staticInventory) Available(sku string) (int, error) {
	n, ok := s.stock[sku]
	if !ok {
		return 0, &NotFoundError{Entity: "sku", ID: sku}
	}
	return n, nil
}

type countingInventory struct {
	inner      staticInventory
	priceCalls int
	stockCalls int
}

func (c *countingInventory) UnitPriceCents(sku string) (int, error) {
	c.priceCalls++
	return c.inner.UnitPriceCents(sku)
}

func (c *countingInventory) Available(sku string) (int, error) {
	c.stockCalls++
	return c.inner.Available(sku)
}

type staticDiscounts struct {
	percent int
}

func (s staticDiscounts) PercentOff(customerID, sku string) (int, error) {
	return s.percent, nil
}

type failingDiscounts struct {
	err error
}

func (f failingDiscounts) PercentOff(string, string) (int, error) {
	return 0, f.err
}

type allowCredit struct{}

func (allowCredit) Authorize(c Customer, amount int) error {
	if c.CreditLimit < c.BalanceCents+amount {
		return &PaymentError{CustomerID: c.ID, Reason: "over limit"}
	}
	return nil
}

type recordingStore struct {
	saves int
}

func (r *recordingStore) Save(PlacedOrder) error {
	r.saves++
	return nil
}

type recordingAuditor struct {
	calls int
}

func (r *recordingAuditor) OrderPlaced(PlacedOrder) {
	r.calls++
}
