//nolint:ireturn // Exported policy factories intentionally return DelayPolicy.
package retry

import (
	"math"
	"math/rand/v2"
	"time"
)

type (
	// randomRangePolicy returns a random delay within a configured range.
	randomRangePolicy struct {
		// minDelay is the lower range bound.
		minDelay time.Duration
		// maxDelay is the upper range bound.
		maxDelay time.Duration
	}

	// exponentialByAttemptPolicy applies exponential delay growth without jitter.
	exponentialByAttemptPolicy struct {
		// baseDelay is the initial delay for exponential growth.
		baseDelay time.Duration
		// maxDelay is the upper delay bound.
		maxDelay time.Duration
	}

	// exponentialBoundedJitterPolicy adds bounded jitter to exponential delay.
	exponentialBoundedJitterPolicy struct {
		// basePolicy is the base exponential policy without jitter.
		basePolicy *exponentialByAttemptPolicy
		// jitterDivisor scales the jitter range relative to baseDelay.
		jitterDivisor uint64
	}

	// exponentialFullJitterPolicy applies full jitter to exponential delay.
	exponentialFullJitterPolicy struct {
		// basePolicy is the base exponential policy without jitter.
		basePolicy *exponentialByAttemptPolicy
	}

	// exponentialEqualJitterPolicy samples delay in [base/2, base) after exponential growth.
	exponentialEqualJitterPolicy struct {
		// basePolicy is the base exponential policy without jitter.
		basePolicy *exponentialByAttemptPolicy
	}
)

// NewRandomRangePolicy returns a random-range delay policy.
// Delay is sampled in the [minDelay, maxDelay) interval.
func NewRandomRangePolicy(minDelay, maxDelay time.Duration) DelayPolicy {
	return &randomRangePolicy{
		minDelay: minDelay,
		maxDelay: maxDelay,
	}
}

// Delay returns a random delay in the configured range.
func (p *randomRangePolicy) Delay(uint64) time.Duration {
	return p.randomDelay()
}

// randomDelay returns a random delay in the configured range.
// Reversed bounds are swapped before sampling.
func (p *randomRangePolicy) randomDelay() time.Duration {
	minDelay := p.minDelay
	maxDelay := p.maxDelay

	if minDelay > maxDelay {
		minDelay, maxDelay = maxDelay, minDelay
	}

	if minDelay == maxDelay {
		return minDelay
	}

	//nolint:gosec // A regular PRNG is sufficient for jitter.
	return minDelay + time.Duration(rand.Int64N(int64(maxDelay-minDelay)))
}

// NewExponentialByAttemptPolicy returns an exponential policy without jitter.
func NewExponentialByAttemptPolicy(baseDelay, maxDelay time.Duration) DelayPolicy {
	return &exponentialByAttemptPolicy{
		baseDelay: baseDelay,
		maxDelay:  maxDelay,
	}
}

// Delay returns exponential delay for the given attempt.
func (p *exponentialByAttemptPolicy) Delay(attempt uint64) time.Duration {
	return p.exponentialDelayByAttempt(attempt)
}

// exponentialDelayByAttempt computes exponential delay by attempt number.
// attempt is one-based, while zero is treated as the first attempt.
func (p *exponentialByAttemptPolicy) exponentialDelayByAttempt(attempt uint64) time.Duration {
	if attempt == 0 {
		return p.exponentialDelayByStep(0)
	}

	return p.exponentialDelayByStep(attempt - 1)
}

// exponentialDelayByStep computes exponential delay by growth step.
// step is zero-based and scales delay by 2^step, capped by maxDelay.
func (p *exponentialByAttemptPolicy) exponentialDelayByStep(step uint64) time.Duration {
	if p.baseDelay <= 0 {
		return 0
	}

	if p.maxDelay <= 0 {
		return p.baseDelay
	}

	if p.baseDelay >= p.maxDelay {
		return p.maxDelay
	}

	delay := p.baseDelay
	for range step {
		if delay > p.maxDelay/2 {
			return p.maxDelay
		}

		delay *= 2
	}

	return min(delay, p.maxDelay)
}

// NewExponentialBoundedJitterPolicy returns an exponential policy with bounded jitter.
func NewExponentialBoundedJitterPolicy(baseDelay, maxDelay time.Duration, jitterDivisor uint64) DelayPolicy {
	return &exponentialBoundedJitterPolicy{
		basePolicy: &exponentialByAttemptPolicy{
			baseDelay: baseDelay,
			maxDelay:  maxDelay,
		},
		jitterDivisor: jitterDivisor,
	}
}

// Delay returns bounded-jitter delay for the given attempt.
func (p *exponentialBoundedJitterPolicy) Delay(attempt uint64) time.Duration {
	return p.addJitterByFraction(p.basePolicy.Delay(attempt))
}

// addJitterByFraction adds random jitter up to baseDelay/jitterDivisor.
func (p *exponentialBoundedJitterPolicy) addJitterByFraction(baseDelay time.Duration) time.Duration {
	if baseDelay <= 0 {
		return 0
	}

	if p.jitterDivisor == 0 || p.jitterDivisor > math.MaxInt64 {
		return baseDelay
	}

	maxJitter := baseDelay / time.Duration(p.jitterDivisor)
	if maxJitter == 0 {
		return baseDelay
	}

	//nolint:gosec // A regular PRNG is sufficient for jitter.
	jitter := time.Duration(rand.Int64N(int64(maxJitter) + 1))

	return baseDelay + jitter
}

// NewExponentialFullJitterPolicy returns an exponential policy with full jitter.
func NewExponentialFullJitterPolicy(baseDelay, maxDelay time.Duration) DelayPolicy {
	return &exponentialFullJitterPolicy{
		basePolicy: &exponentialByAttemptPolicy{
			baseDelay: baseDelay,
			maxDelay:  maxDelay,
		},
	}
}

// Delay returns full-jitter delay for the given attempt.
func (p *exponentialFullJitterPolicy) Delay(attempt uint64) time.Duration {
	return p.fullJitter(p.basePolicy.Delay(attempt))
}

// fullJitter picks a random delay in the range [0, delay).
func (p *exponentialFullJitterPolicy) fullJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}

	//nolint:gosec // A regular PRNG is sufficient for jitter.
	return time.Duration(rand.Int64N(int64(delay)))
}

// NewExponentialEqualJitterPolicy returns an exponential policy with equal jitter.
func NewExponentialEqualJitterPolicy(baseDelay, maxDelay time.Duration) DelayPolicy {
	return &exponentialEqualJitterPolicy{
		basePolicy: &exponentialByAttemptPolicy{
			baseDelay: baseDelay,
			maxDelay:  maxDelay,
		},
	}
}

// Delay returns equal-jitter delay for the given attempt.
func (p *exponentialEqualJitterPolicy) Delay(attempt uint64) time.Duration {
	return p.equalJitter(p.basePolicy.Delay(attempt))
}

// equalJitter picks a random delay in the range [delay/2, delay).
func (p *exponentialEqualJitterPolicy) equalJitter(delay time.Duration) time.Duration {
	if delay <= 1 {
		return delay
	}

	half := delay / 2

	//nolint:gosec // A regular PRNG is sufficient for jitter.
	return half + time.Duration(rand.Int64N(int64(delay-half)))
}
