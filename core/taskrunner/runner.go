package taskrunner

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/security"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

type Runner struct {
	Store        *store.Store
	Concurrency  int
	Capabilities []string
	LeaseTTL     time.Duration
	IdleDelay    time.Duration
	Process      func(context.Context, store.Task)
	OnError      func(string)
}

// Run joins all processors before returning. Requests and browser navigation do
// not own this context; only shutdown or loss of the durable lease cancels it.
func (r Runner) Run(ctx context.Context) error {
	if r.Store == nil || r.Process == nil || r.Concurrency < 1 || r.Concurrency > 100 {
		return errors.New("invalid job runner configuration")
	}
	if r.LeaseTTL == 0 {
		r.LeaseTTL = 2 * time.Minute
	}
	if r.IdleDelay == 0 {
		r.IdleDelay = time.Second
	}
	if r.LeaseTTL < 30*time.Second || r.LeaseTTL > 30*time.Minute || r.IdleDelay < 50*time.Millisecond {
		return errors.New("invalid runner interval")
	}
	owner := security.NewID("server")
	var wg sync.WaitGroup
	for i := 0; i < r.Concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.loop(ctx, owner) }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := r.Store.RecoverExpiredTaskLeases(ctx); err != nil && ctx.Err() == nil {
					r.report("task_recovery_failed")
				}
			}
		}
	}()
	wg.Wait()
	return nil
}

func (r Runner) loop(ctx context.Context, owner string) {
	for ctx.Err() == nil {
		task, err := r.Store.ClaimTaskCapabilities(ctx, owner, r.LeaseTTL, r.Capabilities)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
				r.report("task_claim_failed")
			}
			if !sleep(ctx, r.IdleDelay) {
				return
			}
			continue
		}
		r.process(ctx, task)
	}
}

func (r Runner) process(parent context.Context, t store.Task) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	renewDone := make(chan struct{})
	stopRenew := make(chan struct{})
	go func() {
		defer close(renewDone)
		ticker := time.NewTicker(r.LeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopRenew:
				return
			case <-ticker.C:
				if err := r.Store.RenewTask(ctx, t, r.LeaseTTL); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	func() {
		defer func() {
			if recover() != nil {
				r.report("task_processor_panic")
			}
		}()
		r.Process(ctx, t)
	}()
	close(stopRenew)
	cancel()
	<-renewDone
}

func (r Runner) report(code string) {
	if r.OnError != nil {
		r.OnError(code)
	}
}
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
