package edge

import "time"

type Limits struct {
	HeaderBytes  int64
	BodyBytes    int64
	JSONDepth    int
	ParseTimeout time.Duration
}

func DefaultLimits() Limits {
	return Limits{HeaderBytes: 32 << 10, BodyBytes: 1 << 20, JSONDepth: 64, ParseTimeout: 2 * time.Second}
}
