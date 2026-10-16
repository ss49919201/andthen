package result

import (
	"errors"
	"fmt"
	"testing"
)

var errSentinel = errors.New("sentinel")

func TestFrom(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		err     error
		wantVal int
		wantErr error
	}{
		{name: "ok", value: 3, err: nil, wantVal: 3},
		{name: "err", value: 0, err: errSentinel, wantErr: errSentinel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotErr := From(tt.value, tt.err).Get()
			if gotVal != tt.wantVal {
				t.Fatalf("value = %v, want %v", gotVal, tt.wantVal)
			}
			if !sameError(gotErr, tt.wantErr) {
				t.Fatalf("error = %v, want %v", gotErr, tt.wantErr)
			}
		})
	}
}

func TestMapChain(t *testing.T) {
	tests := []struct {
		name    string
		start   Result[int]
		want    int
		wantErr error
	}{
		{
			name:  "ok chain changes type",
			start: Ok(2),
			want:  3,
		},
		{
			name:    "err chain short-circuits",
			start:   Err[int](errSentinel),
			wantErr: errSentinel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mapCalls int
			out := tt.start.
				Map(func(n int) string { return fmt.Sprintf("%d", n) }).
				Map(func(s string) int {
					mapCalls++
					if s != "2" {
						t.Fatalf("unexpected string %q", s)
					}
					return len(s) * 3
				})
			got, err := out.Get()
			if got != tt.want {
				t.Fatalf("value = %v, want %v", got, tt.want)
			}
			if !sameError(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && mapCalls != 0 {
				t.Fatalf("Map ran %d times on Err, want 0", mapCalls)
			}
		})
	}
}

func TestMapErr(t *testing.T) {
	wrapped := errors.New("wrapped")
	tests := []struct {
		name      string
		start     Result[int]
		wantVal   int
		wantErr   error
		wantCalls int
	}{
		{
			name:      "ok skips fn",
			start:     Ok(5),
			wantVal:   5,
			wantCalls: 0,
		},
		{
			name: "err transforms error",
			start: Err[int](errSentinel),
			wantErr: wrapped,
			wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mapErrCalls int
			out := tt.start.MapErr(func(err error) error {
				mapErrCalls++
				if err != errSentinel {
					t.Fatalf("unexpected err %v", err)
				}
				return wrapped
			})
			got, err := out.Get()
			if got != tt.wantVal {
				t.Fatalf("value = %v, want %v", got, tt.wantVal)
			}
			if !sameError(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if mapErrCalls != tt.wantCalls {
				t.Fatalf("MapErr fn calls = %d, want %d", mapErrCalls, tt.wantCalls)
			}
		})
	}
}

func TestAndThenShortCircuit(t *testing.T) {
	var andThenCalls int
	out := Err[int](errSentinel).AndThen(func(int) Result[string] {
		andThenCalls++
		return Ok("never")
	})
	if _, err := out.Get(); !sameError(err, errSentinel) {
		t.Fatalf("error = %v", err)
	}
	if andThenCalls != 0 {
		t.Fatalf("AndThen ran %d times on Err, want 0", andThenCalls)
	}

	chain := Ok(1).AndThen(func(n int) Result[int] {
		if n != 1 {
			t.Fatalf("n = %d", n)
		}
		return Ok(n + 1)
	}).AndThen(func(n int) Result[int] {
		return Err[int](fmt.Errorf("step2 %d", n))
	})
	if _, err := chain.Get(); err == nil || err.Error() != "step2 2" {
		t.Fatalf("error = %v", err)
	}
}

func TestAndTeeShortCircuit(t *testing.T) {
	var teeCalls int
	original := Ok(7)
	out := original.AndTee(func(n int) {
		teeCalls++
		if n != 7 {
			t.Fatalf("tee n = %d", n)
		}
	})
	if got, err := out.Get(); err != nil || got != 7 {
		t.Fatalf("got (%v, %v)", got, err)
	}
	if teeCalls != 1 {
		t.Fatalf("tee calls = %d", teeCalls)
	}

	teeCalls = 0
	out = Err[int](errSentinel).AndTee(func(int) { teeCalls++ })
	if _, err := out.Get(); !sameError(err, errSentinel) {
		t.Fatalf("error = %v", err)
	}
	if teeCalls != 0 {
		t.Fatalf("AndTee ran on Err, want skip")
	}
}

func TestAndThrough(t *testing.T) {
	t.Run("propagates inner err keeps type", func(t *testing.T) {
		var throughCalls int
		out := Ok("payload").AndThrough(func(s string) Result[int] {
			throughCalls++
			if s != "payload" {
				t.Fatalf("s = %q", s)
			}
			return Err[int](errSentinel)
		})
		got, err := out.Get()
		if got != "" {
			t.Fatalf("value = %q, want zero", got)
		}
		if !sameError(err, errSentinel) {
			t.Fatalf("error = %v", err)
		}
		if throughCalls != 1 {
			t.Fatalf("through calls = %d", throughCalls)
		}
	})

	t.Run("ok inner passes original", func(t *testing.T) {
		out := Ok(42).AndThrough(func(n int) Result[struct{}] {
			if n != 42 {
				t.Fatalf("n = %d", n)
			}
			return Ok(struct{}{})
		})
		got, err := out.Get()
		if err != nil || got != 42 {
			t.Fatalf("got (%v, %v)", got, err)
		}
	})

	t.Run("skips on outer err", func(t *testing.T) {
		var throughCalls int
		out := Err[int](errSentinel).AndThrough(func(int) Result[bool] {
			throughCalls++
			return Ok(true)
		})
		if _, err := out.Get(); !sameError(err, errSentinel) {
			t.Fatalf("error = %v", err)
		}
		if throughCalls != 0 {
			t.Fatalf("AndThrough ran on Err")
		}
	})
}

func TestOrElseAndMatch(t *testing.T) {
	recovered := Err[int](errSentinel).OrElse(func(error) Result[int] {
		return Ok(99)
	})
	if got, err := recovered.Get(); err != nil || got != 99 {
		t.Fatalf("OrElse got (%v, %v)", got, err)
	}

	unchanged := Ok(3).OrElse(func(error) Result[int] {
		t.Fatal("OrElse on Ok should not run")
		return Err[int](errors.New("no"))
	})
	if got, err := unchanged.Get(); err != nil || got != 3 {
		t.Fatalf("unchanged (%v, %v)", got, err)
	}

	mapped := Ok(2).Match(
		func(n int) string { return fmt.Sprintf("ok:%d", n) },
		func(error) string { return "err" },
	)
	if mapped != "ok:2" {
		t.Fatalf("Match ok = %q", mapped)
	}
	mapped = Err[int](errSentinel).Match(
		func(int) string { return "ok" },
		func(error) string { return "err:sentinel" },
	)
	if mapped != "err:sentinel" {
		t.Fatalf("Match err = %q", mapped)
	}
}

func sameError(got, want error) bool {
	if got == nil && want == nil {
		return true
	}
	if got == nil || want == nil {
		return false
	}
	return got == want
}
