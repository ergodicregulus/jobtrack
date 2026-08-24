package jobs

import (
	"context"
	"sync"
	"time"
)

// hostLimiter enforces per-host politeness.
//
// Two properties, and both matter:
//
//  1. Never more than one in-flight request per host. Global concurrency can be
//     high — that is our throughput — but per-host concurrency is exactly 1,
//     because that is their server.
//
//  2. The delay ADAPTS to observed response time. A host that takes 2s to
//     answer gets a longer gap than one answering in 50ms, which backs off
//     automatically when a server is struggling without hard-coding per-domain
//     rules we would never keep up to date.
type hostLimiter struct {
	mu    sync.Mutex
	hosts map[string]*hostState
	base  time.Duration
}

type hostState struct {
	// gate serialises requests to this host. Buffered with capacity 1, so
	// acquiring it is "take the single slot".
	gate chan struct{}
	// nextAllowed is when the next request may start.
	nextAllowed time.Time
	lastSeen    time.Time
}

func newHostLimiter(base time.Duration) *hostLimiter {
	l := &hostLimiter{hosts: make(map[string]*hostState), base: base}
	go l.reap()
	return l
}

// reap drops idle host state. Without it the map grows for every host ever
// seen, turning a politeness mechanism into a memory leak.
func (l *hostLimiter) reap() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		l.mu.Lock()
		for host, st := range l.hosts {
			// Only remove hosts with a free gate, or an in-flight request would
			// lose its slot.
			if st.lastSeen.Before(cutoff) && len(st.gate) == 0 {
				delete(l.hosts, host)
			}
		}
		l.mu.Unlock()
	}
}

// acquire blocks until it is polite to call host, and returns a release
// function that must be given the observed response duration.
func (l *hostLimiter) acquire(ctx context.Context, host string) (release func(time.Duration), err error) {
	l.mu.Lock()
	st, ok := l.hosts[host]
	if !ok {
		st = &hostState{gate: make(chan struct{}, 1)}
		l.hosts[host] = st
	}
	st.lastSeen = time.Now()
	l.mu.Unlock()

	// Take the host's single slot.
	select {
	case st.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	l.mu.Lock()
	wait := time.Until(st.nextAllowed)
	l.mu.Unlock()

	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			<-st.gate // give the slot back, or the host is blocked forever
			return nil, ctx.Err()
		}
	}

	return func(observed time.Duration) {
		l.mu.Lock()
		// Wait a multiple of how long they took, floored at the base delay.
		// A slow server gets more room; a fast one is not punished.
		delay := max(l.base, observed*2)
		if delay > 30*time.Second {
			delay = 30 * time.Second // never stall a worker indefinitely
		}
		st.nextAllowed = time.Now().Add(delay)
		l.mu.Unlock()
		<-st.gate
	}, nil
}
