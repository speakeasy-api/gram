// Package metrics accumulates bounded, payload-free tunnel aggregates. It has
// no dependency on Pub/Sub, ClickHouse, or the customer-side agent.
package metrics

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var LatencyBounds = [...]int64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000}

const MaxSeries = 100000

// maxSources bounds every per-source set the collector keeps. MaxSeries
// reserves room for one coverage row per tracked source.
const maxSources = 10000

type Key struct {
	SourceID, ServerID, Kind, Method, ClientFamily string
	Bucket                                         int64
}
type Snapshot struct {
	Key
	ProducerID                                                           string
	Revision                                                             uint64
	Attempts, Successes, Errors, Canceled, Incomplete, ConnectionsOpened uint64
	// Rejected counts requests Speakeasy refused before they reached the MCP
	// server. They are neither errors nor completions, so they get no latency bin.
	Rejected                           uint64
	LatencyBins                        [12]uint64
	Connections, Consumers, Substreams uint32
}
type Collector struct {
	mu           sync.Mutex
	producerID   string
	series       map[Key]Snapshot
	published    map[Key]uint64
	sources      map[string]time.Time
	now          func() time.Time
	gaugeSources map[string]time.Time
	// drops counts discarded aggregates per source since producer boot. Entries
	// are never removed: the read side marks loss where a source's watermark
	// grew versus this producer's previous row, so a shrinking count would hide
	// later loss. At most maxSources entries, about 1 MB.
	drops map[string]uint64
	// overflowDrops counts drops for sources first seen once drops is full. It
	// is added to every source's watermark, so in that extreme case loss marks
	// every source on this producer as partial, as a producer-wide count would.
	overflowDrops uint64
}

func New() *Collector {
	return &Collector{producerID: uuid.NewString(), series: make(map[Key]Snapshot), published: make(map[Key]uint64), sources: make(map[string]time.Time), now: time.Now, gaugeSources: make(map[string]time.Time), drops: make(map[string]uint64), overflowDrops: 0}
}
func Method(value string) string {
	switch value {
	case "tools/call", "tools/list", "initialize", "resources/read", "resources/list", "prompts/list", "prompts/get", "ping":
		return value
	default:
		return "other"
	}
}

// ClientFamily classifies an MCP clientInfo.name or an HTTP User-Agent. It
// inspects only a bounded prefix and retains only a fixed family label.
func ClientFamily(value string) string {
	value = strings.ToLower(value[:min(len(value), 256)])
	switch {
	case strings.Contains(value, "claude"):
		return "claude"
	case strings.Contains(value, "codex"):
		return "codex"
	case strings.Contains(value, "cursor"):
		return "cursor"
	case strings.Contains(value, "chatgpt"):
		return "chatgpt"
	case strings.Contains(value, "visual studio code"), strings.Contains(value, "vscode"):
		// After cursor: Cursor reports itself as "cursor-vscode".
		return "vscode"
	case strings.Contains(value, "mcp-inspector"):
		return "inspector"
	case value == "":
		return "unknown"
	default:
		return "other"
	}
}

// PreferredFamily picks between families classified from several sources,
// most trusted first: the first named family wins, then "other" when any
// source identified some client, else "unknown". An empty family means the
// source was absent.
func PreferredFamily(families ...string) string {
	result := "unknown"
	for _, family := range families {
		switch family {
		case "", "unknown":
		case "other":
			result = "other"
		default:
			return family
		}
	}
	return result
}

func (c *Collector) Observe(source, server, method, client, outcome string, duration time.Duration) {
	if c == nil || source == "" {
		return
	}
	key := Key{SourceID: source, ServerID: server, Kind: "requests", Method: Method(method), ClientFamily: boundedFamily(client), Bucket: c.now().UTC().Truncate(time.Minute).Unix()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.sources[source]; exists || len(c.sources) < maxSources {
		c.sources[source] = c.now()
	}
	row, ok := c.series[key]
	if !ok && len(c.series) >= MaxSeries-maxSources {
		c.drop(source)
		return
	}
	row.Key = key
	row.ProducerID = c.producerID
	row.Revision++
	switch outcome {
	case "attempt":
		row.Attempts++
	case "success":
		row.Successes++
	case "error":
		row.Errors++
	case "canceled":
		row.Canceled++
	case "rejected":
		row.Rejected++
	case "unrouted":
		// No agent was connected, so the request failed without a response
		// time. It counts as an error but gets no latency bin.
		row.Errors++
	default:
		row.Incomplete++
	}
	if outcome == "success" || outcome == "error" || outcome == "canceled" {
		i := 0
		for i < len(LatencyBounds) && duration > time.Duration(LatencyBounds[i])*time.Millisecond {
			i++
		}
		row.LatencyBins[i]++
	}
	c.series[key] = row
}
func (c *Collector) Connections(source string, connections, consumers, substreams uint32, opened uint64) {
	if c == nil {
		return
	}
	key := Key{SourceID: source, Kind: "connections", Bucket: c.now().UTC().Truncate(15 * time.Second).Unix()}
	c.mu.Lock()
	defer c.mu.Unlock()
	if connections > 0 {
		if _, exists := c.gaugeSources[source]; exists || len(c.gaugeSources) < maxSources {
			c.gaugeSources[source] = c.now()
		}
	}
	row, ok := c.series[key]
	if !ok && len(c.series) >= MaxSeries-maxSources {
		c.drop(source)
		return
	}
	row.Key = key
	row.ProducerID = c.producerID
	row.Revision++
	row.Connections, row.Consumers, row.Substreams = connections, consumers, substreams
	row.ConnectionsOpened += opened
	row.Incomplete = c.watermark(source)
	c.series[key] = row
}

// drop records one discarded aggregate for source. The caller holds c.mu.
func (c *Collector) drop(source string) {
	if _, exists := c.drops[source]; exists || len(c.drops) < maxSources {
		c.drops[source]++
		return
	}
	c.overflowDrops++
}

// watermark is source's loss count since producer boot, never decreasing.
// The caller holds c.mu.
func (c *Collector) watermark(source string) uint64 {
	return c.drops[source] + c.overflowDrops
}

// Run exports cumulative revisions. Failed publishes remain retryable for ten
// minutes. Recording does no I/O; publisher latency never holds the mutex.
func (c *Collector) Run(ctx context.Context, publish func(context.Context, Snapshot) error) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Flush(ctx, publish)
		}
	}
}
func (c *Collector) Flush(ctx context.Context, publish func(context.Context, Snapshot) error) {
	now := c.now().UTC()
	c.mu.Lock()
	// Heartbeats prove this producer is still collecting sources it has observed.
	// They do not prove that every replica is healthy. Reserve space for them.
	for source, lastObserved := range c.sources {
		// Idle sources expire so churn cannot permanently fill this bounded set.
		if now.Sub(lastObserved) > 10*time.Minute {
			delete(c.sources, source)
			continue
		}
		key := Key{SourceID: source, Kind: "coverage", Bucket: now.Truncate(time.Minute).Unix()}
		row, exists := c.series[key]
		if !exists && len(c.series) >= MaxSeries {
			continue
		}
		row.Key, row.ProducerID = key, c.producerID
		row.Revision++
		// For coverage rows only, incomplete is this source's loss watermark
		// since producer boot. Never add it to request outcome counters.
		row.Incomplete = c.watermark(source)
		c.series[key] = row
	}
	batch := make([]Snapshot, 0, len(c.series))
	for key, row := range c.series {
		if now.Unix()-key.Bucket > 600 {
			if c.published[key] < row.Revision {
				c.drop(key.SourceID)
			}
			delete(c.series, key)
			delete(c.published, key)
			continue
		}
		if row.Revision <= c.published[key] {
			if now.Unix()-key.Bucket >= 75 {
				delete(c.series, key)
				delete(c.published, key)
			}
			continue
		}
		batch = append(batch, row)
	}
	c.mu.Unlock()
	// Bound both SDK concurrency and one flush's wall time. Failed and unsent
	// revisions remain in memory for the next pass, within the ten-minute limit.
	flushCtx, stop := context.WithTimeout(ctx, 12*time.Second)
	defer stop()
	jobs := make(chan Snapshot)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for row := range jobs {
				publishCtx, cancel := context.WithTimeout(flushCtx, 2*time.Second)
				err := publish(publishCtx, row)
				cancel()
				if err != nil {
					continue
				}
				c.mu.Lock()
				if latest, ok := c.series[row.Key]; ok {
					c.published[row.Key] = max(c.published[row.Key], row.Revision)
					if latest.Revision == row.Revision && now.Unix()-row.Bucket >= 75 {
						delete(c.series, row.Key)
						delete(c.published, row.Key)
					}
				}
				c.mu.Unlock()
			}
		})
	}
send:
	for _, row := range batch {
		select {
		case jobs <- row:
		case <-flushCtx.Done():
			break send
		}
	}
	close(jobs)
	workers.Wait()
}

func boundedFamily(value string) string {
	switch value {
	case "claude", "codex", "cursor", "chatgpt", "vscode", "inspector", "unknown", "other":
		return value
	default:
		return "other"
	}
}

// GaugeSources keeps disconnected sources for five minutes so the gateway can
// report zero connections. After that, missing samples leave gaps in history.
func (c *Collector) GaugeSources() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]string, 0, len(c.gaugeSources))
	for source, at := range c.gaugeSources {
		if c.now().Sub(at) > 5*time.Minute {
			delete(c.gaugeSources, source)
		} else {
			result = append(result, source)
		}
	}
	return result
}
