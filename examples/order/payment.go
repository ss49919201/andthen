package order

import (
	"errors"

	"github.com/ss49919201/andthen/result"
)

// PaymentAuthorization records a successful charge.
type PaymentAuthorization struct {
	ID      string
	Gateway string
}

// PaidOrder is a priced order with an captured payment.
type PaidOrder struct {
	Priced    PricedOrder
	PaymentID string
	Gateway   string
}

// PaymentGateway charges the customer (primary processor).
type PaymentGateway interface {
	Charge(po PricedOrder) (PaymentAuthorization, error)
}

// Refunder reverses a captured payment during compensation.
type Refunder interface {
	Refund(paymentID string, amountCents int) error
}

// Notifier emits side effects when an order cannot complete.
type Notifier interface {
	OrderFailed(customerID string, cause error)
}

func (s *Service) chargePrimary(po PricedOrder) result.Result[PaidOrder] {
	if s.Payments == nil {
		return result.Ok(PaidOrder{Priced: po, PaymentID: "pay-none", Gateway: "none"})
	}
	auth, err := s.Payments.Charge(po)
	if err != nil {
		return result.Err[PaidOrder](err)
	}
	return result.Ok(PaidOrder{
		Priced:    po,
		PaymentID: auth.ID,
		Gateway:   auth.Gateway,
	})
}

func (s *Service) chargeSecondary(po PricedOrder) result.Result[PaidOrder] {
	if s.SecondaryPayments == nil {
		return result.Err[PaidOrder](ErrPaymentTimeout)
	}
	auth, err := s.SecondaryPayments.Charge(po)
	if err != nil {
		return result.Err[PaidOrder](err)
	}
	return result.Ok(PaidOrder{
		Priced:    po,
		PaymentID: auth.ID,
		Gateway:   auth.Gateway,
	})
}

func (s *Service) recoverPayment(po PricedOrder) func(error) result.Result[PaidOrder] {
	return func(err error) result.Result[PaidOrder] {
		if errors.Is(err, ErrCardDeclined) {
			if s.Notify != nil {
				customerID := po.Stocked.CustomerOrder.Customer.ID
				s.Notify.OrderFailed(customerID, err)
			}
			return result.Err[PaidOrder](err)
		}
		if !errors.Is(err, ErrPaymentTimeout) {
			return result.Err[PaidOrder](err)
		}
		// Timeout: retry primary once, then fall back to a secondary gateway sub-pipeline.
		return s.chargePrimary(po).
			OrElse(func(retryErr error) result.Result[PaidOrder] {
				if !errors.Is(retryErr, ErrPaymentTimeout) {
					return result.Err[PaidOrder](retryErr)
				}
				return s.chargeSecondary(po)
			})
	}
}

func commitReservation(inv InventoryService, refunds Refunder, paid PaidOrder) result.Result[PaidOrder] {
	if paid.Priced.Stocked.Backorder {
		return result.Ok(paid)
	}
	req := paid.Priced.Stocked.CustomerOrder.Request
	if err := inv.CommitReservation(req.SKU, req.Quantity); err != nil {
		if refunds != nil {
			_ = refunds.Refund(paid.PaymentID, paid.Priced.TotalCents)
		}
		return result.Err[PaidOrder](err)
	}
	return result.Ok(paid)
}
