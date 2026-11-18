//go:build !darwin

package audio

import "time"

// outputLatency is a guess at how long audio takes from leaving oto's player
// to being heard: the device buffer plus a little for the hardware.
func outputLatency() time.Duration {
	return deviceBuffer + 20*time.Millisecond
}
