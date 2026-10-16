package main

import (
	"fmt"

	"github.com/ss49919201/andthen/examples/order"
)

func main() {
	svc := demoService()

	scenarios := []struct {
		name string
		req  order.RawRequest
	}{
		{name: "member happy path", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"}},
		{name: "guest checkout", req: order.RawRequest{CustomerID: "guest-1", SKU: "sku-widget", Quantity: "2"}},
		{name: "large order approval", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "4"}},
		{name: "backorder sku", req: order.RawRequest{CustomerID: "c1", SKU: "sku-backorder", Quantity: "1"}},
		{name: "bad quantity", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "nope"}},
		{name: "out of stock (no backorder)", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "99"}},
	}

	for _, sc := range scenarios {
		resp := svc.Handle(sc.req)
		fmt.Printf("[%s] %d %s\n", sc.name, resp.StatusCode, resp.Body)
	}
}

func demoService() *order.Service {
	return &order.Service{
		Customers: demoCustomers{},
		Inventory: demoInventory{},
		Catalog:   demoCatalog{},
		Discounts: demoDiscounts{},
		Credit:    demoCredit{},
		Approvals: demoApprovals{},
		Orders:    demoOrders{},
		Audit:     demoAudit{},
	}
}

type demoCustomers struct{}

func (demoCustomers) FindByID(id string) (order.Customer, error) {
	switch id {
	case "c1":
		return order.Customer{ID: "c1", Kind: "member", Tier: "gold", CreditLimit: 100_000, BalanceCents: 0}, nil
	case "guest-1":
		return order.Customer{ID: "guest-1", Kind: "guest", CreditLimit: 100_000, BalanceCents: 0}, nil
	default:
		return order.Customer{}, &order.NotFoundError{Entity: "customer", ID: id}
	}
}

type demoInventory struct{}

func (demoInventory) UnitPriceCents(sku string) (int, error) {
	switch sku {
	case "sku-widget", "sku-backorder":
		return 1250, nil
	default:
		return 0, &order.NotFoundError{Entity: "sku", ID: sku}
	}
}

func (demoInventory) Available(sku string) (int, error) {
	switch sku {
	case "sku-widget":
		return 12, nil
	case "sku-backorder":
		return 0, nil
	default:
		return 0, &order.NotFoundError{Entity: "sku", ID: sku}
	}
}

type demoCatalog struct{}

func (demoCatalog) AllowsBackorder(sku string) bool {
	return sku == "sku-backorder"
}

type demoDiscounts struct{}

func (demoDiscounts) PercentOff(customerID, sku string) (int, error) {
	if customerID == "c1" && sku == "sku-widget" {
		return 20, nil
	}
	return 0, order.ErrDiscountUnavailable
}

type demoCredit struct{}

func (demoCredit) Authorize(c order.Customer, amount int) error {
	if c.BalanceCents+amount > c.CreditLimit {
		return &order.PaymentError{CustomerID: c.ID, Reason: "over limit"}
	}
	return nil
}

type demoApprovals struct{}

func (demoApprovals) ApproveHighValue(c order.Customer, amount int) error {
	fmt.Printf("  approval: %s high-value order %d cents\n", c.ID, amount)
	return nil
}

type demoOrders struct{}

func (demoOrders) Save(o order.PlacedOrder) error {
	return nil
}

type demoAudit struct{}

func (demoAudit) OrderPlaced(o order.PlacedOrder) {
	fmt.Printf("  audit: placed %s for customer %s\n", o.ID, o.CustomerID)
}
