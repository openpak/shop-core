package nximport

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openpak/shop-core/internal/store"
)

// Mirror imports the Switch catalog into the store, on demand and on a timer.
type Mirror struct {
	Client *Client
	Store  *store.Store
	mu     sync.Mutex // one import at a time
}

// Run imports once and records the outcome in the run ledger.
func (m *Mirror) Run(ctx context.Context) (added, updated int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, rerr := m.Store.StartRun(ctx, "switch", "import", "shop-core")
	items, err := m.Client.Fetch(ctx)
	if err == nil {
		added, updated, err = m.Store.ApplyImport(ctx, "switch", items)
	}
	if rerr == nil {
		state, sum := "ok", ""
		if err != nil {
			state, sum = "failed", err.Error()
		} else {
			sum = summary(added, updated)
		}
		_ = m.Store.FinishRun(ctx, "switch", run.ID, state, sum)
	}
	return added, updated, err
}

func summary(added, updated int) string {
	return fmt.Sprintf("added %d, refreshed %d", added, updated)
}

// Loop imports every interval until ctx ends.
func (m *Mirror) Loop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if a, u, err := m.Run(ctx); err != nil {
			slog.Warn("switch import failed", "err", err)
		} else {
			slog.Info("switch import", "added", a, "refreshed", u)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
