package order

import (
	"testing"
)

func baseService() *Service {
	return &Service{
		Customers: staticCustomers{Customer{ID: "c1", Kind: customerKindMember, Tier: "gold", CreditLimit: 50_000, BalanceCents: 0}},
		Inventory: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 10}},
		Catalog:   staticCatalog{backorder: map[string]bool{"sku-widget": false, "sku-backorder": true}},
		Discounts: staticDiscounts{percent: 10},
		Credit:    allowCredit{},
		Approvals: &recordingApproval{},
		Payments:  &successPayments{gateway: "primary"},
		Refunds:   &recordingRefunds{},
		Orders:    &recordingStore{},
	}
}

func TestPlaceOrder_success_memberPricing(t *testing.T) {
	audit := &recordingAuditor{}
	svc := baseService()
	svc.Audit = audit

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	// 2 * 1000 with 10% member discount = 1800 (< threshold, no manual approval)
	if resp.Body != "order ord-c1-sku-widget placed total=1800" {
		t.Fatalf("body = %q", resp.Body)
	}
	if audit.calls != 1 {
		t.Fatalf("audit calls = %d, want 1", audit.calls)
	}
}

func TestPlaceOrder_guestPricingBranch(t *testing.T) {
	svc := baseService()
	svc.Customers = staticCustomers{Customer{ID: "guest-1", Kind: customerKindGuest, CreditLimit: 50_000}}

	resp := svc.Handle(RawRequest{CustomerID: "guest-1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	// Guest path: 2000 subtotal + 5% fee = 2100 (no member discount service)
	if resp.Body != "order ord-guest-1-sku-widget placed total=2100" {
		t.Fatalf("body = %q", resp.Body)
	}
}

func TestPlaceOrder_smallOrder_skipsManualApproval(t *testing.T) {
	approvals := &recordingApproval{}
	svc := baseService()
	svc.Approvals = approvals

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if approvals.calls != 0 {
		t.Fatalf("approval calls = %d, want 0 for sub-threshold order", approvals.calls)
	}
}

func TestPlaceOrder_largeOrder_requiresManualApproval(t *testing.T) {
	approvals := &recordingApproval{}
	svc := baseService()
	svc.Approvals = approvals

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "4"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	// 4 * 1000 - 10% = 3600 > 3000 threshold
	if approvals.calls != 1 {
		t.Fatalf("approval calls = %d, want 1", approvals.calls)
	}
	if resp.Body != "order ord-c1-sku-widget placed total=3600" {
		t.Fatalf("body = %q", resp.Body)
	}
}

func TestPlaceOrder_backorderSKU_recoversFromStockError(t *testing.T) {
	svc := baseService()
	svc.Inventory = staticInventory{
		prices: map[string]int{"sku-backorder": 1000},
		stock:  map[string]int{"sku-backorder": 0},
	}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-backorder", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	if resp.Body != "order ord-c1-sku-backorder backorder placed total=1800" {
		t.Fatalf("body = %q", resp.Body)
	}
}

func TestPlaceOrder_invalidQuantity_shortCircuitsInventory(t *testing.T) {
	inv := &countingInventory{
		inner: staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 10}},
	}
	svc := baseService()
	svc.Inventory = inv

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

func TestPlaceOrder_outOfStock_nonBackorderSKU_fails(t *testing.T) {
	store := &recordingStore{}
	svc := baseService()
	svc.Orders = store
	svc.Inventory = staticInventory{prices: map[string]int{"sku-widget": 1000}, stock: map[string]int{"sku-widget": 1}}

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "5"})
	if resp.StatusCode != 409 {
		t.Fatalf("status = %d, want 409; body %q", resp.StatusCode, resp.Body)
	}
	if store.saves != 0 {
		t.Fatalf("persist ran %d times, want 0", store.saves)
	}
}

func TestPlaceOrder_paymentTimeout_retryRejoinsChain(t *testing.T) {
	store := &recordingStore{}
	gw := &scriptedPayments{gateway: "primary", failUntilCall: 1, failWith: ErrPaymentTimeout}
	svc := baseService()
	svc.Payments = gw
	svc.Orders = store

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	if gw.calls != 2 {
		t.Fatalf("primary payment calls = %d, want 2 (initial + retry)", gw.calls)
	}
	if store.saves != 1 {
		t.Fatalf("persist saves = %d, want 1 after payment recovery", store.saves)
	}
}

func TestPlaceOrder_paymentTimeout_fallsBackToSecondaryGateway(t *testing.T) {
	primary := &scriptedPayments{gateway: "primary", alwaysFail: ErrPaymentTimeout}
	secondary := &successPayments{gateway: "secondary"}
	store := &recordingStore{}
	svc := baseService()
	svc.Payments = primary
	svc.SecondaryPayments = secondary
	svc.Orders = store

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, want 201; body %q", resp.StatusCode, resp.Body)
	}
	if primary.calls != 2 {
		t.Fatalf("primary calls = %d, want 2 before fallback", primary.calls)
	}
	if secondary.charges != 1 {
		t.Fatalf("secondary charges = %d, want 1", secondary.charges)
	}
	if store.saves != 1 {
		t.Fatalf("persist saves = %d, want 1 after secondary payment", store.saves)
	}
}

func TestPlaceOrder_cardDeclined_shortCircuitsAndNotifies(t *testing.T) {
	gw := &scriptedPayments{gateway: "primary", alwaysFail: ErrCardDeclined}
	notify := &recordingNotifier{}
	store := &recordingStore{}
	svc := baseService()
	svc.Payments = gw
	svc.Notify = notify
	svc.Orders = store

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 402 {
		t.Fatalf("status = %d, want 402; body %q", resp.StatusCode, resp.Body)
	}
	if resp.Body != ErrCardDeclined.Error() {
		t.Fatalf("body = %q", resp.Body)
	}
	if notify.failures != 1 {
		t.Fatalf("notify failures = %d, want 1", notify.failures)
	}
	if gw.calls != 1 {
		t.Fatalf("payment calls = %d, want 1 (no retry on decline)", gw.calls)
	}
	if store.saves != 0 {
		t.Fatalf("persist saves = %d, want 0", store.saves)
	}
}

func TestPlaceOrder_commitReservationFailure_refundsAndReturnsStockError(t *testing.T) {
	stockErr := &StockError{SKU: "sku-widget", Requested: 2, Available: 0}
	refunds := &recordingRefunds{}
	store := &recordingStore{}
	svc := baseService()
	svc.Inventory = staticInventory{
		prices:     map[string]int{"sku-widget": 1000},
		stock:      map[string]int{"sku-widget": 10},
		commitFail: stockErr,
	}
	svc.Refunds = refunds
	svc.Orders = store

	resp := svc.Handle(RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"})
	if resp.StatusCode != 409 {
		t.Fatalf("status = %d, want 409; body %q", resp.StatusCode, resp.Body)
	}
	if resp.Body != stockErr.Error() {
		t.Fatalf("body = %q", resp.Body)
	}
	if refunds.calls != 1 {
		t.Fatalf("refund calls = %d, want 1", refunds.calls)
	}
	if refunds.lastPaymentID == "" {
		t.Fatal("expected refund payment id")
	}
	if refunds.lastAmount != 1800 {
		t.Fatalf("refund amount = %d, want 1800", refunds.lastAmount)
	}
	if store.saves != 0 {
		t.Fatalf("persist saves = %d, want 0", store.saves)
	}
}

func TestPlaceOrder_discountUnavailable_recoversWithOrElse(t *testing.T) {
	svc := baseService()
	svc.Discounts = failingDiscounts{err: ErrDiscountUnavailable}

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

type staticCatalog struct {
	backorder map[string]bool
}

func (s staticCatalog) AllowsBackorder(sku string) bool {
	return s.backorder[sku]
}

type staticInventory struct {
	prices     map[string]int
	stock      map[string]int
	commitFail error
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

func (s staticInventory) CommitReservation(sku string, qty int) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	avail, err := s.Available(sku)
	if err != nil {
		return err
	}
	if avail < qty {
		return &StockError{SKU: sku, Requested: qty, Available: avail}
	}
	return nil
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

func (c *countingInventory) CommitReservation(sku string, qty int) error {
	return c.inner.CommitReservation(sku, qty)
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

type recordingApproval struct {
	calls int
}

func (r *recordingApproval) ApproveHighValue(Customer, int) error {
	r.calls++
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

type successPayments struct {
	gateway string
	charges int
}

func (s *successPayments) Charge(PricedOrder) (PaymentAuthorization, error) {
	s.charges++
	return PaymentAuthorization{ID: "pay-ok", Gateway: s.gateway}, nil
}

type scriptedPayments struct {
	gateway       string
	calls         int
	failUntilCall int
	failWith      error
	alwaysFail    error
}

func (g *scriptedPayments) Charge(PricedOrder) (PaymentAuthorization, error) {
	g.calls++
	if g.alwaysFail != nil {
		return PaymentAuthorization{}, g.alwaysFail
	}
	if g.calls <= g.failUntilCall {
		return PaymentAuthorization{}, g.failWith
	}
	return PaymentAuthorization{ID: "pay-retry", Gateway: g.gateway}, nil
}

type recordingRefunds struct {
	calls           int
	lastPaymentID   string
	lastAmount      int
}

func (r *recordingRefunds) Refund(paymentID string, amountCents int) error {
	r.calls++
	r.lastPaymentID = paymentID
	r.lastAmount = amountCents
	return nil
}

type recordingNotifier struct {
	failures int
}

func (n *recordingNotifier) OrderFailed(string, error) {
	n.failures++
}
