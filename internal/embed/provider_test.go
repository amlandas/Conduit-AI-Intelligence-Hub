package embed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetryCall_SucceedsFirstTry(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryCall(context.Background(), DefaultRetryPolicy(), zeroJitter, func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("retryCall: %v", err)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
}

func TestRetryCall_DoesNotRetryPermanentErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("permanent")
	calls := 0
	err := retryCall(context.Background(), RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond}, zeroJitter,
		func(ctx context.Context) error {
			calls++
			return sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1 (permanent errors must not retry)", calls)
	}
}

func TestRetryCall_RetriesTransientUpToLimit(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("flaky")
	calls := 0
	err := retryCall(context.Background(), RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
		zeroJitter, func(ctx context.Context) error {
			calls++
			return markTransient(sentinel)
		})
	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to wrap %v", err, sentinel)
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3", calls)
	}
	// The retry marker must not leak into the surfaced error.
	if isTransient(err) {
		t.Error("surfaced error still carries the transient marker")
	}
}

func TestRetryCall_StopsAtContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	calls := 0
	start := time.Now()
	err := retryCall(ctx, RetryPolicy{MaxAttempts: 100, BaseDelay: 20 * time.Millisecond, MaxDelay: 40 * time.Millisecond},
		zeroJitter, func(ctx context.Context) error {
			calls++
			return markTransient(errors.New("always fails"))
		})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a deadline error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("retry loop ran for %s, should have stopped at the 60ms deadline", elapsed)
	}
	if calls >= 100 {
		t.Errorf("made %d calls; the deadline should have cut the loop short", calls)
	}
}

func TestRetryCall_RecoversAfterTransientFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	err := retryCall(context.Background(), RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
		zeroJitter, func(ctx context.Context) error {
			calls++
			if calls < 3 {
				return markTransient(errors.New("not yet"))
			}
			return nil
		})
	if err != nil {
		t.Fatalf("retryCall: %v", err)
	}
	if calls != 3 {
		t.Errorf("called %d times, want 3", calls)
	}
}

func TestRetryPolicy_BackoffIsBoundedAndIncreasing(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{MaxAttempts: 10, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond}.normalize()

	if d := p.delayFor(0, zeroJitter); d != 0 {
		t.Errorf("delayFor(0) = %s, want 0 (first attempt is immediate)", d)
	}
	var prev time.Duration
	for attempt := 1; attempt <= 8; attempt++ {
		d := p.delayFor(attempt, oneJitter)
		if d > p.MaxDelay {
			t.Errorf("delayFor(%d) = %s exceeds MaxDelay %s", attempt, d, p.MaxDelay)
		}
		if attempt > 1 && d < prev {
			t.Errorf("delayFor(%d) = %s is less than previous %s", attempt, d, prev)
		}
		prev = d
	}
}

func TestRetryPolicy_NormalizeFillsZeroValues(t *testing.T) {
	t.Parallel()

	p := RetryPolicy{}.normalize()
	if p.MaxAttempts < 1 || p.BaseDelay <= 0 || p.MaxDelay < p.BaseDelay {
		t.Fatalf("normalize produced an unusable policy: %+v", p)
	}
}

func TestChunkTexts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		texts []string
		size  int
		want  [][]string
	}{
		{"exact multiple", []string{"a", "b", "c", "d"}, 2, [][]string{{"a", "b"}, {"c", "d"}}},
		{"with remainder", []string{"a", "b", "c"}, 2, [][]string{{"a", "b"}, {"c"}}},
		{"single batch", []string{"a", "b"}, 10, [][]string{{"a", "b"}}},
		{"empty input", nil, 4, nil},
		{"non positive size falls back", []string{"a"}, 0, [][]string{{"a"}}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := chunkTexts(tc.texts, tc.size)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("chunkTexts(%v, %d) = %v, want %v", tc.texts, tc.size, got, tc.want)
			}
		})
	}
}

func TestUnwrapTransient(t *testing.T) {
	t.Parallel()

	base := errors.New("inner")
	if got := unwrapTransient(markTransient(base)); got != base {
		t.Errorf("unwrapTransient = %v, want %v", got, base)
	}
	if got := unwrapTransient(base); got != base {
		t.Errorf("unwrapTransient on a plain error = %v, want %v", got, base)
	}
	if got := unwrapTransient(nil); got != nil {
		t.Errorf("unwrapTransient(nil) = %v, want nil", got)
	}
}

// zeroJitter and oneJitter make backoff deterministic in tests.
func zeroJitter() float64 { return 0 }
func oneJitter() float64  { return 1 }
