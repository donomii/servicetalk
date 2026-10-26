package main

import (
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

type runtimeCounterSet struct {
	callsIn      atomic.Uint64
	callsOut     atomic.Uint64
	callTimeouts atomic.Uint64
	queueFull    atomic.Uint64
}

var runtimeCounters runtimeCounterSet

func runtimeMetricsSnapshot() RuntimeMetrics {
	return RuntimeMetrics{
		CallsIn:      runtimeCounters.callsIn.Load(),
		CallsOut:     runtimeCounters.callsOut.Load(),
		CallTimeouts: runtimeCounters.callTimeouts.Load(),
		QueueFull:    runtimeCounters.queueFull.Load(),
	}
}

func recordNATSError(err error) {
	if err == nil {
		return
	}
	if err == nats.ErrTimeout || err == nats.ErrNoResponders {
		runtimeCounters.callTimeouts.Add(1)
	}
}

type coordinatorCounterSet struct {
	spawnCount        atomic.Uint64
	spawnErrorCount   atomic.Uint64
	spawnLatencyMsSum atomic.Uint64
	spawnLatencyMsMax atomic.Uint64
	pulseCount        atomic.Uint64
	pulseErrorCount   atomic.Uint64
	pulseLatencyMsSum atomic.Uint64
	pulseLatencyMsMax atomic.Uint64
}

func (c *Coordinator) recordSpawnLatency(start time.Time, err error) {
	ms := uint64(time.Since(start).Milliseconds())
	c.metrics.spawnCount.Add(1)
	c.metrics.spawnLatencyMsSum.Add(ms)
	recordMax(&c.metrics.spawnLatencyMsMax, ms)
	if err != nil {
		c.metrics.spawnErrorCount.Add(1)
	}
}

func (c *Coordinator) recordPulseLatency(start time.Time, err error) {
	ms := uint64(time.Since(start).Milliseconds())
	c.metrics.pulseCount.Add(1)
	c.metrics.pulseLatencyMsSum.Add(ms)
	recordMax(&c.metrics.pulseLatencyMsMax, ms)
	if err != nil {
		c.metrics.pulseErrorCount.Add(1)
	}
}

func (c *Coordinator) coordinatorMetricsSnapshot() CoordinatorMetrics {
	return CoordinatorMetrics{
		SpawnCount:        c.metrics.spawnCount.Load(),
		SpawnErrorCount:   c.metrics.spawnErrorCount.Load(),
		SpawnLatencyMsSum: c.metrics.spawnLatencyMsSum.Load(),
		SpawnLatencyMsMax: c.metrics.spawnLatencyMsMax.Load(),
		PulseCount:        c.metrics.pulseCount.Load(),
		PulseErrorCount:   c.metrics.pulseErrorCount.Load(),
		PulseLatencyMsSum: c.metrics.pulseLatencyMsSum.Load(),
		PulseLatencyMsMax: c.metrics.pulseLatencyMsMax.Load(),
	}
}

func recordMax(slot *atomic.Uint64, value uint64) {
	for {
		old := slot.Load()
		if value <= old {
			return
		}
		if slot.CompareAndSwap(old, value) {
			return
		}
	}
}
