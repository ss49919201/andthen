package result

// Result is a success value or an error. Err short-circuits Map, AndThen, AndTee, AndThrough, and MapErr skips its fn on Ok.
type Result[T any] struct {
	value T
	err   error
}

// Ok wraps a success value.
func Ok[T any](value T) Result[T] {
	return Result[T]{value: value}
}

// Err wraps a failure. The success slot is zero.
func Err[T any](err error) Result[T] {
	return Result[T]{err: err}
}

// From builds a Result from the usual Go (T, error) pair.
func From[T any](value T, err error) Result[T] {
	if err != nil {
		return Err[T](err)
	}
	return Ok(value)
}

// Get returns the success value and error, like a plain Go return.
func (r Result[T]) Get() (T, error) {
	return r.value, r.err
}

// Match applies ok or err and returns the unified result.
func (r Result[T]) Match[U any](ok func(T) U, onErr func(error) U) U {
	if r.err != nil {
		return onErr(r.err)
	}
	return ok(r.value)
}

// Map transforms the success value when this result is Ok.
func (r Result[T]) Map[U any](fn func(T) U) Result[U] {
	if r.err != nil {
		return Err[U](r.err)
	}
	return Ok(fn(r.value))
}

// MapErr transforms the error when this result is Err; Ok is returned unchanged.
func (r Result[T]) MapErr(fn func(error) error) Result[T] {
	if r.err != nil {
		return Err[T](fn(r.err))
	}
	return r
}

// AndThen chains another Result-producing step on the success value.
func (r Result[T]) AndThen[U any](fn func(T) Result[U]) Result[U] {
	if r.err != nil {
		return Err[U](r.err)
	}
	return fn(r.value)
}

// AndTee runs fn on the success value for side effects and returns the original result.
func (r Result[T]) AndTee(fn func(T)) Result[T] {
	if r.err != nil {
		return r
	}
	fn(r.value)
	return r
}

// AndThrough runs fn on the success value. An Err from fn is propagated; Ok values are discarded.
func (r Result[T]) AndThrough[X any](fn func(T) Result[X]) Result[T] {
	if r.err != nil {
		return r
	}
	if inner := fn(r.value); inner.err != nil {
		return Err[T](inner.err)
	}
	return r
}

// OrElse recovers from Err by returning fn's result; Ok is unchanged.
func (r Result[T]) OrElse(fn func(error) Result[T]) Result[T] {
	if r.err != nil {
		return fn(r.err)
	}
	return r
}
