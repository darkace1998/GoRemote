package app

import (
	"context"
	"time"
)

// persisterLoop is the debounced persistence goroutine. It is started by
// App.Start and stops when persistSig is closed (by Shutdown).
//
// Behaviour:
//   - Waits for a signal or a tick of DefaultPersistInterval.
//   - When the dirty flag is set, flushes the snapshot to disk.
//   - Transient save errors are logged; the dirty flag is left set so the
//     next tick retries.
func (a *App) persisterLoop() {
	defer close(a.persistDone)
	interval := a.cfg.PersistInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-a.rootCtx.Done():
			return
		case _, ok := <-a.persistSig:
			if !ok {
				return
			}
			// A mutation happened. Wait for the debounce interval before
			// flushing so bursts of mutations are coalesced.
			select {
			case <-a.rootCtx.Done():
				return
			case <-time.After(interval):
			}
			a.tryFlush()
		case <-ticker.C:
			a.tryFlush()
		}
	}
}

func (a *App) tryFlush() {
	if !a.dirty.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.flushNow(ctx); err != nil {
		a.logger.Error("debounced persist failed", errAttr(err))
	}
}
