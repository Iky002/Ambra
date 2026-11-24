package ticks

import "time"

// TicksPerSecond is the .NET / Emby tick unit (100 nanoseconds).
const TicksPerSecond int64 = 10_000_000

func FromDuration(d time.Duration) int64 {
	return int64(d / 100)
}

func FromSeconds(sec float64) int64 {
	return int64(sec * float64(TicksPerSecond))
}

func ToDuration(t int64) time.Duration {
	return time.Duration(t) * 100 * time.Nanosecond
}

func ToSeconds(t int64) float64 {
	return float64(t) / float64(TicksPerSecond)
}
