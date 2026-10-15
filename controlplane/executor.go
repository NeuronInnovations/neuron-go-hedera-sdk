package controlplane

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Priority int

const (
	PriorityHigh Priority = iota
	PriorityNormal
	PriorityLow
)

type task struct {
	fn func(context.Context)
}

type Executor struct {
	ctx    context.Context
	highQ  chan task
	normQ  chan task
	lowQ   chan task
	wg     sync.WaitGroup
}

func NewExecutor(ctx context.Context, workers int, queueSize int) *Executor {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 64
	}
	e := &Executor{
		ctx:   ctx,
		highQ: make(chan task, queueSize),
		normQ: make(chan task, queueSize),
		lowQ:  make(chan task, queueSize),
	}
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go e.worker()
	}
	return e
}

func (e *Executor) worker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		// Prefer high-priority work whenever available.
		select {
		case t := <-e.highQ:
			t.fn(e.ctx)
			continue
		default:
		}

		select {
		case <-e.ctx.Done():
			return
		case t := <-e.highQ:
			t.fn(e.ctx)
		case t := <-e.normQ:
			t.fn(e.ctx)
		case t := <-e.lowQ:
			t.fn(e.ctx)
		}
	}
}

func (e *Executor) Submit(priority Priority, timeout time.Duration, fn func(context.Context)) error {
	if fn == nil {
		return fmt.Errorf("controlplane: nil task")
	}
	q := e.normQ
	switch priority {
	case PriorityHigh:
		q = e.highQ
	case PriorityLow:
		q = e.lowQ
	}
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-e.ctx.Done():
		return fmt.Errorf("controlplane: executor stopped")
	case q <- task{fn: fn}:
		return nil
	case <-timer.C:
		return fmt.Errorf("controlplane: queue full")
	}
}

