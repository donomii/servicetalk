package main

import "time"

// Local arrival time measures the lease; a remote sweep ID only orders pulses.
func (obj *ObjectServer) recordPulseArrival(now time.Time) {
	obj.pulseClockMu.Lock()
	defer obj.pulseClockMu.Unlock()
	if now.After(obj.lastPulseAt) {
		obj.lastPulseAt = now
	} else {
		// Concurrent receivers must not move the local lease backwards.
	}
}

func (obj *ObjectServer) pulseAge(now time.Time) time.Duration {
	obj.pulseClockMu.Lock()
	defer obj.pulseClockMu.Unlock()
	return now.Sub(obj.lastPulseAt)
}
