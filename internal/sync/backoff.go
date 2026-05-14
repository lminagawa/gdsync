package sync

import (
	"context"
	"math"
	"math/rand"
	"time"
)

type BackoffPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	MaxAttempts  int
	Jitter       float64
}

func DefaultPolicy(maxAttempts int) BackoffPolicy {
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	return BackoffPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		MaxAttempts:  maxAttempts,
		Jitter:       0.2,
	}
}

func (p BackoffPolicy) Do(ctx context.Context, op func() error) error {
	var lastErr error
	delay := p.InitialDelay
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if !IsRetryable(err) {
			return err
		}
		if attempt == p.MaxAttempts {
			break
		}
		d := jitter(delay, p.Jitter)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
		delay = time.Duration(math.Min(float64(p.MaxDelay), float64(delay)*p.Multiplier))
	}
	return lastErr
}

func jitter(base time.Duration, ratio float64) time.Duration {
	if ratio <= 0 {
		return base
	}
	factor := 1.0 + (rand.Float64()*2-1)*ratio
	if factor < 0 {
		factor = 0
	}
	return time.Duration(float64(base) * factor)
}
