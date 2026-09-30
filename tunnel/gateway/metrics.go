package gateway

import (
	"context"
	"time"
)

// RunMetrics samples all sessions owned by this gateway at aligned intervals.
// History leaves gaps where no gateway reported a sample.
func (g *Gateway) RunMetrics(ctx context.Context) {
	if g.cfg.Metrics == nil {
		return
	}
	for {
		timer := time.NewTimer(time.Until(time.Now().Truncate(15 * time.Second).Add(15 * time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		sources := map[string]struct{}{}
		for _, source := range g.reg.liveTunnelIDs() {
			sources[source] = struct{}{}
		}
		for _, source := range g.cfg.Metrics.GaugeSources() {
			sources[source] = struct{}{}
		}
		for source := range sources {
			g.recordMetrics(source, 0)
		}
	}
}
func (g *Gateway) recordMetrics(source string, opened uint64) {
	if g.cfg.Metrics == nil {
		return
	}
	var consumers, substreams uint32
	now := time.Now()
	connections := g.reg.connections(source, now)
	for _, conn := range connections {
		consumers += uint32(conn.ActiveConsumerSessions)
		substreams += uint32(conn.ActiveSubstreams)
	}
	g.cfg.Metrics.Connections(source, uint32(len(connections)), consumers, substreams, opened)
}
