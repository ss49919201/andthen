# andthen

A small Go library of [neverthrow](https://github.com/supermacro/neverthrow)-style `Result` types, using Go generics for fluent error handling without nested `if err != nil` checks.

## Install

```bash
go get github.com/ss49919201/andthen
```

Requires Go 1.27 or later.

## Usage

```go
package main

import (
	"fmt"
	"strconv"

	"github.com/ss49919201/andthen/result"
)

func main() {
	n, err := result.From(strconv.Atoi("42")).
		Map(func(n int) int { return n * 2 }).
		AndThen(func(n int) result.Result[int] {
			if n < 0 {
				return result.Err[int](fmt.Errorf("negative %d", n))
			}
			return result.Ok(n)
		}).
		Get()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(n) // 84
}
```

Simpler starting point:

```go
r := result.From(strconv.Atoi("42"))
value, err := r.Get()
```

### API

| Function / method | Role |
|-------------------|------|
| `Ok`, `Err`, `From` | Construct a `Result` |
| `Get` | Unwrap to `(T, error)` |
| `Map` | Transform the success value |
| `AndThen` | Chain another `Result`-returning step |
| `AndTee` | Side effect on success; keeps the same `Result` |
| `AndThrough` | Run a step that must succeed; discards its value |
| `MapErr` | Transform the error |
| `OrElse` | Recover from `Err` |
| `Match` | Branch on ok vs err |

When a `Result` is `Err`, `Map`, `AndThen`, `AndTee`, `AndThrough`, and `MapErr` short-circuit (they do not run your callback). `OrElse` is the recovery path on `Err`.

## Example: order placement

[`examples/order`](examples/order) is a runnable, business-style flow: parse and validate a request, look up a customer, reserve stock (with `OrElse` backorder recovery when the catalog allows it), then branch inside `AndThen` for **member vs guest** pricing, a standard credit check (`AndThrough`), and an extra **manual approval** sub-pipeline only when the total exceeds a threshold. Member pricing also uses `OrElse` when the discount service is down. The flow persists the order and emits an audit event (`AndTee`). Domain errors use `errors.Is` / `errors.As`; the handler boundary uses `Match` to build an HTTP-like response.

```bash
go run ./examples/order/demo/
go test ./examples/order/...
```

## Inspired by neverthrow

The chaining model and naming follow [neverthrow](https://github.com/supermacro/neverthrow) (TypeScript). This package adapts that style to Go’s `(T, error)` idiom with generic methods on `Result[T]`.

An `option` package is planned for a later release.
