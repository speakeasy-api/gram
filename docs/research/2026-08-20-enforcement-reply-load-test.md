# Enforcement reply core load test

Date: 2026-08-20

## Result

The replica inbox, single drainer, and waiter map completed every tested Redis-only sweep without a timeout, error, or orphan. The first clear small-reply knee appeared around a burst of 2,500 replies. Above that point, completion rate settled around 90,000 to 100,000 replies/s and p99 grew with queue size. At the requested maximum of 5,000 scans expecting five replies each, the drainer routed 25,000 replies with 257.4 ms p99.

Waiters did not consume Redis connections. At 5,000 concurrent waits, the dedicated drainer used one connection while the shared writer pool reached its configured 128-connection cap. Registering all 5,000 real waiters took 6.5 ms in the baseline run. A mirrored five-round map probe measured 187 ns/op for the mutex map and 154 ns/op for `sync.Map`, only a 1.21x difference at this deliberately extreme concurrency.

A forced one-second drainer pause accumulated 24,999 queued small replies and then caught up in 56.2 ms. A paused queue of 4,999 findings-heavy replies used 109.8 MB above the sampled Redis baseline and caught up in 264.3 ms. Both retained about 59 seconds of TTL at release. The 60-second expiry is therefore not threatened by the tested realistic backlog depths, but it remains a hard loss boundary for a drainer outage approaching one minute.

## Method

The harness is `server/cmd/tools/enforceload`. It creates a fresh replica inbox for each sweep point and reports one CSV row per point plus a terminal summary.

Reply-leg mode registers every `Await` before releasing one synthetic scanner goroutine per scan. Each scanner waits for the configured simulated latency, then writes one to five replies through the real `replyinbox.Writer`. RTT begins when the synthetic requests are released and ends when each `Await` receives its last expected reply. Waiter registration time is reported separately. The measured runs used zero simulated scan latency so scanner time did not obscure the reply leg.

Full-loop mode publishes through the local Pub/Sub emulator, invokes the real gitleaks enforcement handler, writes the reply to Redis, and completes through the same inbox drainer and waiter. These values are a functional and local-emulator stress check only.

The harness sampled Redis `LLEN`, `CLIENT LIST`, pool stats, and `INFO memory`. Inbox counters record decoded orphans, completed BLPOP wake cycles, total drained entries, and largest BLPOP plus LPOP catch-up batch. The drainer-pause probe uses an injected gate immediately after BLPOP. This leaves the drainer holding one reply while Redis accumulates the rest, then releases the same live drainer to measure catch-up.

Configuration common to the Redis sweeps:

| Setting               |                                                      Value |
| --------------------- | ---------------------------------------------------------: |
| Redis                 |                             6.2.22, local Docker container |
| Redis address         |                                           `127.0.0.1:5445` |
| Drainer pool size     |                            2 configured, 1 connection used |
| Drainer BLPOP timeout |                                                        1 s |
| Drainer read timeout  |                                                        2 s |
| Shared writer pool    |                                            128 connections |
| Reply TTL             |                                                       60 s |
| Redis sample interval |                              2 ms, 5 ms for full-loop mode |
| Go                    |                                        1.26.5, linux/arm64 |
| Host allocation       |                             17 available CPUs, 37.8 GB RAM |
| Host                  |                           shared Apple ARM virtual machine |
| Git base              | `1c1a56106b`, with the AIS-402 worktree changes under test |

The host already had about 26.6 GB of RAM in use and 12.0 GB of swap in use. Results are single-run observations on a shared development box, not controlled capacity benchmarks. The sampler itself adds Redis commands and shared-pool connections. Redis memory is sampled `used_memory`, not process RSS, and includes allocator and connection-pool effects.

## Redis-only summary

Small synthetic replies serialized to 41 bytes. `Replies/s` is completed scans/s multiplied by replies expected per scan.

| In-flight scans | Replies/scan | Burst replies | p50 ms | p99 ms | Max ms | Scans/s | Replies/s | Depth high-water | Largest drain batch | Redis clients peak | Timeouts/orphans |
| --------------: | -----------: | ------------: | -----: | -----: | -----: | ------: | --------: | ---------------: | ------------------: | -----------------: | ---------------: |
|              10 |            1 |            10 |   1.42 |   1.43 |   1.43 |   6,989 |     6,989 |                6 |                  10 |                 12 |              0/0 |
|              50 |            1 |            50 |   1.03 |   1.74 |   1.74 |  28,653 |    28,653 |                5 |                  50 |                 29 |              0/0 |
|             100 |            1 |           100 |   3.17 |   5.43 |   5.43 |  18,401 |    18,401 |               27 |                  73 |                 81 |              0/0 |
|             500 |            1 |           500 |  10.35 |  11.68 |  11.68 |  42,816 |    42,816 |              177 |                 500 |                129 |              0/0 |
|           1,000 |            1 |         1,000 |   8.19 |   8.92 |   8.92 | 112,071 |   112,071 |              465 |               1,000 |                129 |              0/0 |
|           2,500 |            1 |         2,500 |  17.89 |  24.18 |  24.19 | 103,329 |   103,329 |              361 |               2,500 |                129 |              0/0 |
|           5,000 |            1 |         5,000 |  40.99 |  46.95 |  46.97 | 106,448 |   106,448 |            1,539 |               5,000 |                129 |              0/0 |
|              10 |            5 |            50 |   2.09 |   2.17 |   2.17 |   4,614 |    23,068 |                0 |                  50 |                 11 |              0/0 |
|              50 |            5 |           250 |   9.18 |  10.32 |  10.32 |   4,845 |    24,227 |              136 |                 250 |                 37 |              0/0 |
|             100 |            5 |           500 |   9.17 |   9.98 |  10.18 |   9,828 |    49,138 |               14 |                 500 |                102 |              0/0 |
|             500 |            5 |         2,500 |  31.44 |  32.33 |  32.68 |  15,292 |    76,462 |               60 |               1,712 |                129 |              0/0 |
|           1,000 |            5 |         5,000 |  50.79 |  53.11 |  53.44 |  18,712 |    93,562 |              256 |               5,000 |                129 |              0/0 |
|           2,500 |            5 |        12,500 | 132.52 | 139.83 | 139.84 |  17,878 |    89,388 |              986 |              12,500 |                129 |              0/0 |
|           5,000 |            5 |        25,000 | 244.78 | 257.44 | 257.52 |  19,412 |    97,062 |            1,596 |              25,000 |                129 |              0/0 |

The non-monotonic low-concurrency rows are scheduler, connection warm-up, and burst-shape noise. The stable high-volume observation is the approximately 90,000 to 100,000 small replies/s plateau. Tail latency begins rising materially once a burst contains roughly 2,500 replies, then grows approximately with the number of replies the single drainer must demultiplex.

## Findings-heavy replies

Each large synthetic reply contained 100 safe findings and serialized to 17,305 bytes.

| In-flight scans | p50 ms | p99 ms | Max ms | Scans/s | Depth high-water | Redis memory increase | Timeouts/orphans |
| --------------: | -----: | -----: | -----: | ------: | ---------------: | --------------------: | ---------------: |
|              10 |   4.79 |   4.89 |   4.89 |   2,043 |                5 |               0.60 MB |              0/0 |
|              50 |  11.78 |  13.24 |  13.24 |   3,776 |               31 |               0.76 MB |              0/0 |
|             100 |  15.20 |  22.67 |  22.67 |   4,411 |               26 |               1.87 MB |              0/0 |
|             500 |  57.41 |  82.99 |  83.00 |   6,024 |              357 |               9.90 MB |              0/0 |
|           1,000 | 118.55 | 152.57 | 152.58 |   6,554 |              951 |              23.00 MB |              0/0 |
|           2,500 | 211.82 | 295.39 | 295.41 |   8,463 |            2,243 |              46.01 MB |              0/0 |
|           5,000 | 367.28 | 503.98 | 505.08 |   9,899 |            4,102 |              84.16 MB |              0/0 |

Payload size, protobuf serialization, Redis network transfer, and unmarshalling become the limiting work before waiter demultiplexing does. At 5,000 scans, the queue held up to 4,102 replies and the sampled Redis increase was 84.2 MB. The separate paused case held 4,999 replies and measured 109.8 MB above baseline, which better captures the full queued footprint.

## Drainer pause and TTL

All pause cases held the drainer for one second after the writers completed. The drainer had already removed one item with BLPOP, so backlog at release is one less than the waiter count.

| Waiters | Findings/reply | Backlog at release | Serialized payload | Catch-up after release | TTL remaining | Redis memory increase | Timeouts/orphans |
| ------: | -------------: | -----------------: | -----------------: | ---------------------: | ------------: | --------------------: | ---------------: |
|   1,000 |              0 |                999 |              41 KB |                 2.9 ms |          59 s |               7.35 MB |              0/0 |
|   5,000 |              0 |              4,999 |             205 KB |                 9.0 ms |          59 s |               7.78 MB |              0/0 |
|  25,000 |              0 |             24,999 |            1.03 MB |                56.2 ms |          59 s |               8.45 MB |              0/0 |
|   5,000 |            100 |              4,999 |            86.5 MB |               264.3 ms |          59 s |             109.81 MB |              0/0 |

The small-reply memory deltas are dominated by expanding the 128-connection writer pool and Redis allocator behavior, not the serialized list contents. The heavy paused case is the useful memory sizing point.

The TTL is refreshed by every writer pipeline. At the tested depths, catch-up consumes far below one second, so queue depth alone does not threaten the 60-second TTL. Because writers refresh the shared key, the loss boundary during a drainer outage is 60 seconds after the last write, not after the outage begins: a stalled drainer with replies still arriving accumulates an unbounded backlog until writes stop, then the whole list expires 60 seconds later. Alert on `risk.enforcement.drainer_alive` going to zero rather than on key expiry; the waiters those replies belong to die with their deadlines long before the list does.

## Waiter map and Redis connections

The actual mutex-protected waiter map registered 5,000 waiters in 6.5 ms in the baseline, 4.8 ms in the five-reply sweep, and 11.7 ms with 100-finding replies. The latter variation happened before reply creation and shows shared-box scheduling noise rather than payload-dependent map work.

The harness also ran five rounds of 5,000 goroutines performing a store and delete against a mirrored mutex map and `sync.Map`. Median results were:

| Map                   | Time per operation | Relative |
| --------------------- | -----------------: | -------: |
| Mutex plus native map |             187 ns |    1.21x |
| `sync.Map`            |             154 ns |    1.00x |

The absolute cost is negligible beside even the smallest measured reply RTT. The mutex implementation retains type safety and simple atomic registration semantics. There is no evidence to replace it at 5,000 waiters.

Waiter count and Redis connection count were independent. With 5,000 active waiters, the dedicated inbox pool used one BLPOP connection. Total Redis clients stopped at 129 because the shared writer pool was explicitly capped at 128 and the drainer added one. The sampling commands reused the shared pool. Waiters themselves held zero connections.

## Full loop through the Pub/Sub emulator

The full-loop sweep used a real gitleaks finding and the real enforcement handler. Every scan completed with one finding-bearing reply, and there were no timeouts, errors, or orphans.

| In-flight scans |   p50 ms |   p99 ms |   Max ms | Scans/s | Inbox depth high-water | Timeouts/orphans |
| --------------: | -------: | -------: | -------: | ------: | ---------------------: | ---------------: |
|              10 |   124.52 |   193.15 |   193.15 |      52 |                      0 |              0/0 |
|              50 |    58.23 |   145.17 |   145.17 |     344 |                      0 |              0/0 |
|             100 |    19.33 |    20.39 |    20.49 |   4,838 |                     13 |              0/0 |
|             500 |   151.10 |   267.86 |   271.46 |   1,834 |                     22 |              0/0 |
|           1,000 |   280.26 |   518.78 |   519.72 |   1,917 |                     41 |              0/0 |
|           2,500 |   662.48 | 1,274.53 | 1,275.77 |   1,952 |                     38 |              0/0 |
|           5,000 | 1,291.87 | 2,554.23 | 2,557.29 |   1,950 |                     88 |              0/0 |

The 100-scan row is much faster than the 10 and 50 rows because the local emulator and publisher batch more efficiently at that burst size. Above 1,000 scans, emulator plus gitleaks throughput plateaus near 1,950 scans/s. This is not a prediction of GCP Pub/Sub latency, tail behavior, or production scanner capacity. It only proves the topology and implementation remain functional at the requested local concurrency.

## Scaling cliffs

### 1. Single-drainer demultiplexing

The first repeatable knee is a burst of about 2,500 small replies. With five replies per scan, moving from 500 queued replies to 2,500 moved p99 from 10.0 ms to 32.3 ms. At 5,000 replies p99 was 53.1 ms. Beyond that, completion rate stayed near 90,000 to 100,000 replies/s while p99 grew to 139.8 ms at 12,500 replies and 257.4 ms at 25,000 replies.

This is a graceful queueing cliff, not a failure cliff. No replies were lost and no waiters timed out.

### 2. Waiter-map contention

At 5,000 concurrent registrations, real registration completed in 6.5 ms. The mirrored mutex map was only 33 ns/op slower than `sync.Map`. Map contention is not a meaningful contributor to end-to-end latency at the tested count.

### 3. Redis connection behavior

The dedicated drainer held one connection at every sweep point. The shared writer pool expanded with concurrency and capped at 128. Total clients capped at 129 even with 5,000 waiters. The expected O(1) waiter connection behavior is confirmed.

### 4. Pause, catch-up, and TTL

The largest small-reply backlog, 24,999 Redis list entries, caught up in 56.2 ms. The largest byte backlog, 4,999 replies carrying 100 findings, caught up in 264.3 ms. Both had 59 seconds of TTL remaining. Realistic queue depth does not approach the TTL boundary on this machine, but a roughly minute-long drainer outage does.

### 5. Large replies

Large replies shift the ceiling from reply count toward bytes and protobuf work. At 5,000 concurrent 17.3 KB replies, p99 was 504.0 ms versus 46.9 ms for 41-byte replies. A fully paused queue of the same heavy replies added 109.8 MB to sampled Redis memory. Finding count and reply-size limits should be treated as capacity controls.

## Implications for production sizing

- Expected per-replica concurrency in the tens is far below every observed Redis-side knee. Even 1,000 scans expecting five small replies completed at 53.1 ms p99.
- Keep one drainer per replica. The simpler mutex waiter map and one blocking connection have ample headroom; `sync.Map` would not materially change the result.
- Size and cap non-blocking writer pools independently from waiter count. A 128-connection pool drove the local peak; each scanner process adds its own pool budget in production.
- Monitor reply RTT, inbox depth, orphan count, timeout count, drainer reconnects, and Redis memory. Alert on sustained drainer unavailability well before 60 seconds.
- Bound findings or serialized reply bytes. Heavy replies were approximately 10.7x slower at p99 and consumed over 100 MB for a paused 5,000-item queue.
- Do not use the emulator numbers to choose the five-second gate budget. A preproduction test against real GCP Pub/Sub, production-like subscriber scaling, and network distance is still required because the request leg is likely to dominate tail latency.

## Reproduction

Start only the local dependencies:

```sh
docker compose up -d gram-cache pubsub-emulator
```

Run from the repository root with the `mise.toml` environment loaded:

```sh
mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10,50,100,500,1000,2500,5000 -expected 1 -findings 0 -scan-latency 0 -timeout 30s -redis-pool-size 128 -sample-interval 2ms -csv /tmp/ais402-reply-e1.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10,50,100,500,1000,2500,5000 -expected 5 -findings 0 -scan-latency 0 -timeout 45s -redis-pool-size 128 -sample-interval 2ms -map-probe=false -csv /tmp/ais402-reply-e5.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10,50,100,500,1000,2500,5000 -expected 1 -findings 100 -scan-latency 0 -timeout 60s -redis-pool-size 128 -sample-interval 2ms -map-probe=false -csv /tmp/ais402-reply-large.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10 -expected 1 -findings 0 -timeout 30s -sample-interval 2ms -map-probe=false -pause-backlog 1000 -pause-duration 1s -csv /tmp/ais402-pause-1000.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10 -expected 1 -findings 0 -timeout 30s -sample-interval 2ms -map-probe=false -pause-backlog 5000 -pause-duration 1s -csv /tmp/ais402-pause-5000.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10 -expected 1 -findings 0 -timeout 45s -sample-interval 2ms -map-probe=false -pause-backlog 25000 -pause-duration 1s -csv /tmp/ais402-pause-25000.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode reply -concurrency 10 -expected 1 -findings 100 -timeout 60s -sample-interval 2ms -map-probe=false -pause-backlog 5000 -pause-duration 1s -csv /tmp/ais402-pause-large-5000.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode full -concurrency 10,50,100,500 -expected 1 -timeout 60s -redis-pool-size 128 -sample-interval 5ms -map-probe=false -csv /tmp/ais402-full.csv

mise exec -- go run ./server/cmd/tools/enforceload -mode full -concurrency 1000,2500,5000 -expected 1 -timeout 120s -redis-pool-size 128 -sample-interval 5ms -map-probe=false -csv /tmp/ais402-full-high.csv
```

The first command leaves `-map-probe` enabled and prints the five-round mutex versus `sync.Map` comparison before the sweep.

## Deviations and limits

- The brief allowed SIGSTOP or an injected pause. This run used an injected post-BLPOP gate so only the drainer paused and Redis, writers, samplers, and waiters remained observable.
- The 60-second expiry boundary was not tested by intentionally waiting for expiration. Redis would deterministically delete the backlog. The test instead measured remaining TTL and catch-up at realistic depths.
- Raw CSV files were written to `/tmp` and are not repository artifacts. The tables above preserve all required sweep results.
- The two full-loop invocations use separate fresh emulator projects. Emulator cold start and batching make their low-concurrency rows non-comparable for sizing.
- Results were not repeated into confidence intervals because the goal was cliff discovery on a shared development box. The map comparison alone reports the median of five rounds.
