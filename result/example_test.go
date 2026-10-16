package result_test

import (
	"fmt"
	"strconv"

	"github.com/ss49919201/andthen/result"
)

func Example() {
	parse := func(s string) result.Result[int] {
		return result.From(strconv.Atoi(s))
	}
	validate := func(n int) result.Result[int] {
		if n < 0 {
			return result.Err[int](fmt.Errorf("negative %d", n))
		}
		return result.Ok(n)
	}

	bad := parse("x").AndThen(validate).Map(func(n int) string {
		return fmt.Sprintf("n=%d", n)
	})
	fmt.Println(bad.Match(
		func(string) string { return "transformed" },
		func(err error) string { return "stopped: " + err.Error() },
	))

	good := parse("10").AndThen(validate).Map(func(n int) string {
		return fmt.Sprintf("n=%d", n)
	})
	fmt.Println(good.Match(
		func(s string) string { return s },
		func(error) string { return "" },
	))
	// Output:
	// stopped: strconv.Atoi: parsing "x": invalid syntax
	// n=10
}

func ExampleFrom() {
	v, err := result.From(21, nil).Get()
	fmt.Println(v, err == nil)
	// Output: 21 true
}

func ExampleResult_Map() {
	n, err := result.Ok(2).Map(func(n int) int { return n * 3 }).Get()
	fmt.Println(n, err)
	// Output: 6 <nil>
}

func ExampleResult_AndThen() {
	s, err := result.Ok(1).AndThen(func(n int) result.Result[string] {
		return result.Ok(fmt.Sprintf("%d", n+1))
	}).Get()
	fmt.Println(s, err)
	// Output: 2 <nil>
}

func ExampleResult_AndTee() {
	var logged int
	r := result.Ok(5).AndTee(func(n int) { logged = n })
	v, err := r.Get()
	fmt.Println(v, logged, err)
	// Output: 5 5 <nil>
}

func ExampleResult_AndThrough() {
	v, err := result.Ok("go").AndThrough(func(s string) result.Result[int] {
		return result.Ok(len(s))
	}).Get()
	fmt.Println(v, err)
	// Output: go <nil>
}

func ExampleResult_MapErr() {
	inner := fmt.Errorf("inner")
	_, err := result.Err[int](inner).MapErr(func(e error) error {
		return fmt.Errorf("wrapped: %w", e)
	}).Get()
	fmt.Println(err)
	// Output: wrapped: inner
}

func ExampleResult_OrElse() {
	v, err := result.Err[int](fmt.Errorf("fail")).OrElse(func(error) result.Result[int] {
		return result.Ok(0)
	}).Get()
	fmt.Println(v, err)
	// Output: 0 <nil>
}

func ExampleResult_Match() {
	msg := result.Ok(3).Match(
		func(n int) string { return fmt.Sprintf("ok %d", n) },
		func(error) string { return "err" },
	)
	fmt.Println(msg)
	// Output: ok 3
}
