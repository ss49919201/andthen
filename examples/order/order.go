package order

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ss49919201/andthen/result"
)

const (
	customerKindMember = "member"
	customerKindGuest  = "guest"

	guestCheckoutFeePercent = 5
	largeOrderThresholdCents = 3000
)

// RawRequest is the untrusted payload (for example, JSON fields from HTTP).
type RawRequest struct {
	CustomerID string
	SKU        string
	Quantity   string
}

// ParsedRequest has normalized primitive fields.
type ParsedRequest struct {
	CustomerID string
	SKU        string
	Quantity   int
}

// ValidatedRequest passed business rules for the line item.
type ValidatedRequest struct {
	CustomerID string
	SKU        string
	Quantity   int
}

// CustomerOrder ties a validated line to a customer record.
type CustomerOrder struct {
	Request  ValidatedRequest
	Customer Customer
}

// StockedOrder confirms inventory is available or is a backorder line.
type StockedOrder struct {
	CustomerOrder CustomerOrder
	UnitPrice     int // cents
	Backorder     bool
}

// PricedOrder is StockedOrder plus computed totals (type change at pricing).
type PricedOrder struct {
	Stocked     StockedOrder
	Subtotal    int
	Discount    int
	TotalCents  int
	Recoverable bool // true when discount service failed and we used full price
}

// PlacedOrder is persisted and ready to return to the client.
type PlacedOrder struct {
	ID         string
	CustomerID string
	SKU        string
	Quantity   int
	TotalCents int
	Backorder  bool
}

// Customer is a minimal buyer profile used for pricing and credit checks.
type Customer struct {
	ID           string
	Kind         string // "member" or "guest"
	Tier         string // loyalty tier for members
	CreditLimit  int    // cents
	BalanceCents int
}

// HTTPResponse is a stand-in for an HTTP handler result.
type HTTPResponse struct {
	StatusCode int
	Body       string
}

// CustomerRepository loads customers by ID.
type CustomerRepository interface {
	FindByID(id string) (Customer, error)
}

// InventoryService reports unit price and stock levels.
type InventoryService interface {
	UnitPriceCents(sku string) (int, error)
	Available(sku string) (int, error)
}

// ProductCatalog describes product-specific fulfillment rules.
type ProductCatalog interface {
	AllowsBackorder(sku string) bool
}

// DiscountService returns a member discount percent (0–100) for a customer and SKU.
type DiscountService interface {
	PercentOff(customerID, sku string) (int, error)
}

// CreditChecker validates the buyer can afford the total.
type CreditChecker interface {
	Authorize(customer Customer, amountCents int) error
}

// ApprovalService performs extra checks on high-value orders.
type ApprovalService interface {
	ApproveHighValue(customer Customer, amountCents int) error
}

// OrderStore persists a placed order.
type OrderStore interface {
	Save(order PlacedOrder) error
}

// Auditor records successful placements (side effect only).
type Auditor interface {
	OrderPlaced(order PlacedOrder)
}

// Service wires dependencies for the placement pipeline.
type Service struct {
	Customers CustomerRepository
	Inventory InventoryService
	Catalog   ProductCatalog
	Discounts DiscountService
	Credit    CreditChecker
	Approvals ApprovalService
	Orders    OrderStore
	Audit     Auditor
}

// Handle runs the full placement flow and maps to an HTTP-like response.
func (s *Service) Handle(raw RawRequest) HTTPResponse {
	return s.place(raw).
		MapErr(func(err error) error {
			return &DomainError{Operation: "place_order", Cause: err}
		}).
		Match(
			func(o PlacedOrder) HTTPResponse {
				return HTTPResponse{
					StatusCode: 201,
					Body:       formatPlacedResponse(o),
				}
			},
			func(err error) HTTPResponse {
				return mapErrorToHTTP(err)
			},
		)
}

func formatPlacedResponse(o PlacedOrder) string {
	if o.Backorder {
		return fmt.Sprintf("order %s backorder placed total=%d", o.ID, o.TotalCents)
	}
	return fmt.Sprintf("order %s placed total=%d", o.ID, o.TotalCents)
}

func (s *Service) place(raw RawRequest) result.Result[PlacedOrder] {
	return result.Ok(raw).
		AndThen(parseRequest).
		AndThen(validateRequest).
		AndThen(func(v ValidatedRequest) result.Result[CustomerOrder] {
			return lookupCustomer(s.Customers, v)
		}).
		AndThen(func(co CustomerOrder) result.Result[StockedOrder] {
			return reserveStock(s.Inventory, co).OrElse(recoverBackorder(s.Catalog, s.Inventory, co))
		}).
		AndThen(func(so StockedOrder) result.Result[PricedOrder] {
			return branchPricing(s, so)
		}).
		AndThrough(func(po PricedOrder) result.Result[struct{}] {
			return authorizeCredit(s.Credit, po)
		}).
		AndThen(func(po PricedOrder) result.Result[PricedOrder] {
			return branchApproval(s, po)
		}).
		AndThen(func(po PricedOrder) result.Result[PlacedOrder] {
			return persistOrder(s.Orders, po)
		}).
		AndTee(func(o PlacedOrder) {
			if s.Audit != nil {
				s.Audit.OrderPlaced(o)
			}
		})
}

func branchPricing(s *Service, so StockedOrder) result.Result[PricedOrder] {
	if so.CustomerOrder.Customer.Kind == customerKindGuest {
		return priceGuest(so)
	}
	return priceMember(s.Discounts, so).OrElse(recoverFromDiscountFailure(so))
}

func branchApproval(s *Service, po PricedOrder) result.Result[PricedOrder] {
	if po.TotalCents < largeOrderThresholdCents {
		return result.Ok(po)
	}
	return result.Ok(po).AndThrough(func(p PricedOrder) result.Result[struct{}] {
		if s.Approvals == nil {
			return result.Err[struct{}](&ApprovalError{Reason: "approval service unavailable"})
		}
		if err := s.Approvals.ApproveHighValue(p.Stocked.CustomerOrder.Customer, p.TotalCents); err != nil {
			return result.Err[struct{}](err)
		}
		return result.Ok(struct{}{})
	})
}

func parseRequest(raw RawRequest) result.Result[ParsedRequest] {
	qty, err := strconv.Atoi(strings.TrimSpace(raw.Quantity))
	if err != nil {
		return result.Err[ParsedRequest](&ValidationError{
			Field:  "quantity",
			Detail: "must be an integer",
		})
	}
	sku := strings.TrimSpace(raw.SKU)
	customerID := strings.TrimSpace(raw.CustomerID)
	if customerID == "" || sku == "" {
		return result.Err[ParsedRequest](ErrInvalidInput)
	}
	return result.Ok(ParsedRequest{
		CustomerID: customerID,
		SKU:        sku,
		Quantity:   qty,
	})
}

func validateRequest(p ParsedRequest) result.Result[ValidatedRequest] {
	if p.Quantity <= 0 {
		return result.Err[ValidatedRequest](&ValidationError{
			Field:  "quantity",
			Detail: "must be positive",
		})
	}
	if len(p.SKU) < 3 {
		return result.Err[ValidatedRequest](&ValidationError{
			Field:  "sku",
			Detail: "too short",
		})
	}
	return result.Ok(ValidatedRequest{
		CustomerID: p.CustomerID,
		SKU:        p.SKU,
		Quantity:   p.Quantity,
	})
}

func lookupCustomer(repo CustomerRepository, v ValidatedRequest) result.Result[CustomerOrder] {
	c, err := repo.FindByID(v.CustomerID)
	if err != nil {
		return result.Err[CustomerOrder](err)
	}
	return result.Ok(CustomerOrder{Request: v, Customer: c})
}

func reserveStock(inv InventoryService, co CustomerOrder) result.Result[StockedOrder] {
	unit, err := inv.UnitPriceCents(co.Request.SKU)
	if err != nil {
		return result.Err[StockedOrder](err)
	}
	avail, err := inv.Available(co.Request.SKU)
	if err != nil {
		return result.Err[StockedOrder](err)
	}
	if avail < co.Request.Quantity {
		return result.Err[StockedOrder](&StockError{
			SKU:       co.Request.SKU,
			Requested: co.Request.Quantity,
			Available: avail,
		})
	}
	return result.Ok(StockedOrder{CustomerOrder: co, UnitPrice: unit})
}

func recoverBackorder(cat ProductCatalog, inv InventoryService, co CustomerOrder) func(error) result.Result[StockedOrder] {
	return func(err error) result.Result[StockedOrder] {
		var stock *StockError
		if !errors.As(err, &stock) {
			return result.Err[StockedOrder](err)
		}
		if cat == nil || !cat.AllowsBackorder(co.Request.SKU) {
			return result.Err[StockedOrder](err)
		}
		unit, uerr := inv.UnitPriceCents(co.Request.SKU)
		if uerr != nil {
			return result.Err[StockedOrder](uerr)
		}
		return result.Ok(StockedOrder{
			CustomerOrder: co,
			UnitPrice:     unit,
			Backorder:     true,
		})
	}
}

func priceMember(disc DiscountService, so StockedOrder) result.Result[PricedOrder] {
	percent, err := disc.PercentOff(so.CustomerOrder.Customer.ID, so.CustomerOrder.Request.SKU)
	if err != nil {
		return result.Err[PricedOrder](err)
	}
	subtotal := so.UnitPrice * so.CustomerOrder.Request.Quantity
	discount := subtotal * percent / 100
	return result.Ok(PricedOrder{
		Stocked:     so,
		Subtotal:    subtotal,
		Discount:    discount,
		TotalCents:  subtotal - discount,
		Recoverable: false,
	})
}

func priceGuest(so StockedOrder) result.Result[PricedOrder] {
	subtotal := so.UnitPrice * so.CustomerOrder.Request.Quantity
	fee := subtotal * guestCheckoutFeePercent / 100
	return result.Ok(PricedOrder{
		Stocked:     so,
		Subtotal:    subtotal,
		Discount:    -fee,
		TotalCents:  subtotal + fee,
		Recoverable: false,
	})
}

func recoverFromDiscountFailure(so StockedOrder) func(error) result.Result[PricedOrder] {
	return func(err error) result.Result[PricedOrder] {
		if !errors.Is(err, ErrDiscountUnavailable) {
			return result.Err[PricedOrder](err)
		}
		subtotal := so.UnitPrice * so.CustomerOrder.Request.Quantity
		return result.Ok(PricedOrder{
			Stocked:     so,
			Subtotal:    subtotal,
			Discount:    0,
			TotalCents:  subtotal,
			Recoverable: true,
		})
	}
}

func authorizeCredit(checker CreditChecker, po PricedOrder) result.Result[struct{}] {
	if checker == nil {
		return result.Ok(struct{}{})
	}
	customer := po.Stocked.CustomerOrder.Customer
	if err := checker.Authorize(customer, po.TotalCents); err != nil {
		return result.Err[struct{}](err)
	}
	return result.Ok(struct{}{})
}

func persistOrder(store OrderStore, po PricedOrder) result.Result[PlacedOrder] {
	req := po.Stocked.CustomerOrder.Request
	order := PlacedOrder{
		ID:         fmt.Sprintf("ord-%s-%s", req.CustomerID, req.SKU),
		CustomerID: req.CustomerID,
		SKU:        req.SKU,
		Quantity:   req.Quantity,
		TotalCents: po.TotalCents,
		Backorder:  po.Stocked.Backorder,
	}
	if err := store.Save(order); err != nil {
		return result.Err[PlacedOrder](err)
	}
	return result.Ok(order)
}

func mapErrorToHTTP(err error) HTTPResponse {
	var domain *DomainError
	if errors.As(err, &domain) {
		err = domain.Cause
	}

	var validation *ValidationError
	if errors.As(err, &validation) {
		return HTTPResponse{StatusCode: 400, Body: validation.Error()}
	}
	if errors.Is(err, ErrInvalidInput) {
		return HTTPResponse{StatusCode: 400, Body: err.Error()}
	}

	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return HTTPResponse{StatusCode: 404, Body: notFound.Error()}
	}

	var stock *StockError
	if errors.As(err, &stock) {
		return HTTPResponse{StatusCode: 409, Body: stock.Error()}
	}

	var payment *PaymentError
	if errors.As(err, &payment) {
		return HTTPResponse{StatusCode: 402, Body: payment.Error()}
	}

	var approval *ApprovalError
	if errors.As(err, &approval) {
		return HTTPResponse{StatusCode: 403, Body: approval.Error()}
	}

	return HTTPResponse{StatusCode: 500, Body: "internal error"}
}
