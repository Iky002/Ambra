package ticks

import (
	"math"
	"time"
)

// TicksPerSecond is the .NET / Emby tick unit (100 nanoseconds).
const TicksPerSecond int64 = 10_000_000

const (
	maxDurationTicks = math.MaxInt64 / 100
	minDurationTicks = math.MinInt64 / 100
)

func FromDuration(d time.Duration) int64 {
	return int64(d / 100)
}

func FromSeconds(sec float64) int64 {
	if math.IsNaN(sec) {
		return 0
	}
	v := sec * float64(TicksPerSecond)
	if math.IsInf(v, 1) || v >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if math.IsInf(v, -1) || v <= float64(math.MinInt64) {
		return math.MinInt64
	}
	return int64(v)
}

func ToDuration(t int64) time.Duration {
	if t >= maxDurationTicks {
		return time.Duration(math.MaxInt64)
	}
	if t <= minDurationTicks {
		return time.Duration(math.MinInt64)
	}
	return time.Duration(t) * 100 * time.Nanosecond
}

func ToSeconds(t int64) float64 {
	return float64(t) / float64(TicksPerSecond)
}
