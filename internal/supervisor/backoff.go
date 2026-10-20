package supervisor

import (
	"math/rand/v2"
	"time"
)

func Backoff(attempt int, base, ceiling time.Duration) time.Duration {
	if base <= 0 || ceiling <= 0 {
		return 0
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	maximum := base * time.Duration(1<<attempt)
	if maximum < 0 || maximum > ceiling {
		maximum = ceiling
	}
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}
