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
		{name: "happy path", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "2"}},
		{name: "bad quantity", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "nope"}},
		{name: "out of stock", req: order.RawRequest{CustomerID: "c1", SKU: "sku-widget", Quantity: "99"}},
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
		Discounts: demoDiscounts{},
		Credit:    demoCredit{},
		Orders:    demoOrders{},
		Audit:     demoAudit{},
	}
}

type demoCustomers struct{}

func (demoCustomers) FindByID(id string) (order.Customer, error) {
	if id != "c1" {
		return order.Customer{}, &order.NotFoundError{Entity: "customer", ID: id}
	}
	return order.Customer{ID: "c1", Tier: "gold", CreditLimit: 100_000, BalanceCents: 0}, nil
}

type demoInventory struct{}

func (demoInventory) UnitPriceCents(sku string) (int, error) {
	if sku != "sku-widget" {
		return 0, &order.NotFoundError{Entity: "sku", ID: sku}
	}
	return 1250, nil
}

func (demoInventory) Available(sku string) (int, error) {
	if sku != "sku-widget" {
		return 0, &order.NotFoundError{Entity: "sku", ID: sku}
	}
	return 12, nil
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

type demoOrders struct{}

func (demoOrders) Save(o order.PlacedOrder) error {
	return nil
}

type demoAudit struct{}

func (demoAudit) OrderPlaced(o order.PlacedOrder) {
	fmt.Printf("  audit: placed %s for customer %s\n", o.ID, o.CustomerID)
}
