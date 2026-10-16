package order

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput          = errors.New("invalid input")
	ErrDiscountUnavailable   = errors.New("discount service unavailable")
	ErrInsufficientInventory = errors.New("insufficient inventory")
)

// ValidationError marks client mistakes (bad field values).
type ValidationError struct {
	Field  string
	Detail string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Detail)
}

// NotFoundError is returned when a referenced entity does not exist.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Entity, e.ID)
}

// StockError describes an inventory shortfall.
type StockError struct {
	SKU       string
	Requested int
	Available int
}

func (e *StockError) Error() string {
	return fmt.Sprintf("sku %s: requested %d, available %d", e.SKU, e.Requested, e.Available)
}

// PaymentError means the buyer cannot cover the order total.
type PaymentError struct {
	CustomerID string
	Reason     string
}

func (e *PaymentError) Error() string {
	return fmt.Sprintf("customer %s: %s", e.CustomerID, e.Reason)
}

// DomainError wraps an operational failure for the API boundary.
type DomainError struct {
	Operation string
	Cause     error
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %v", e.Operation, e.Cause)
}

func (e *DomainError) Unwrap() error {
	return e.Cause
}
