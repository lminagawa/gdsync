package sync

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestBackoffSuccessFirstTry(t *testing.T) {
	p := BackoffPolicy{InitialDelay: time.Millisecond, MaxAttempts: 3, Multiplier: 2}
	calls := 0
	if err := p.Do(context.Background(), func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestBackoffRetryThenSuccess(t *testing.T) {
	p := BackoffPolicy{InitialDelay: time.Millisecond, MaxAttempts: 4, Multiplier: 2, MaxDelay: 10 * time.Millisecond}
	calls := 0
	err := p.Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return syscall.EAGAIN
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestBackoffNonRetryableReturnsImmediately(t *testing.T) {
	p := BackoffPolicy{InitialDelay: time.Millisecond, MaxAttempts: 5}
	myErr := errors.New("permanent")
	calls := 0
	err := p.Do(context.Background(), func() error {
		calls++
		return myErr
	})
	if !errors.Is(err, myErr) {
		t.Fatalf("err = %v, want %v", err, myErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (non-retryable should not retry)", calls)
	}
}

func TestBackoffExhaustReturnsLastErr(t *testing.T) {
	p := BackoffPolicy{InitialDelay: time.Millisecond, MaxAttempts: 3, Multiplier: 2, MaxDelay: 5 * time.Millisecond}
	calls := 0
	err := p.Do(context.Background(), func() error {
		calls++
		return syscall.EBUSY
	})
	if !errors.Is(err, syscall.EBUSY) {
		t.Fatalf("err = %v, want EBUSY", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestBackoffContextCancel(t *testing.T) {
	p := BackoffPolicy{InitialDelay: 50 * time.Millisecond, MaxAttempts: 10, Multiplier: 2}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	err := p.Do(ctx, func() error { return syscall.EAGAIN })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
