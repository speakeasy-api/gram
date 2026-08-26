# Request-reply over async messaging: established recipes for reply routing

Research notes for the blocking risk-enforcement scan path (AIS-402): a scan request fans out over GCP Pub/Sub to scanner consumers, and the reply must reach one waiting goroutine on one specific server replica within roughly 5 seconds. The contemplated reply leg is Redis, either (a) a per-scan reply list consumed with `BLPOP`, or (b) a per-replica Redis pub/sub channel with a single subscriber and an in-memory waiter map. This doc surveys how established systems route replies back to the waiting caller instance, and what the primary sources say about each option's failure modes.

Citations marked **[vendor]** are first-party documentation or source; **[community]** is forum/issue-tracker material or secondary writeups.

## 1. The classic patterns (Hohpe/Woolf EIP)

Three patterns compose into every implementation below.

- **Request-Reply**: "a pair of Request-Reply messages, each on its own channel." The reply channel is "almost always point-to-point, because it usually makes no sense to broadcast replies - they should only be returned to the requestor." Two consumption styles: a thread that "sends the request message, blocks (as a Polling Consumer) to wait for the reply," or an async callback where "multiple outstanding requests share a single reply channel." **[vendor]** https://www.enterpriseintegrationpatterns.com/patterns/messaging/RequestReply.html
- **Return Address**: "The request message should contain a Return Address that indicates where to send the reply message." The requestor, not the replier, decides where replies go, which is exactly what makes multi-instance requestors work: each instance names its own reply destination. **[vendor]** https://www.enterpriseintegrationpatterns.com/patterns/messaging/ReturnAddress.html
- **Correlation Identifier**: "Each reply message should contain a Correlation Identifier, a unique identifier that indicates which request message this reply is for." Needed whenever a reply channel is shared by multiple outstanding requests. **[vendor]** https://www.enterpriseintegrationpatterns.com/patterns/messaging/CorrelationIdentifier.html

Option (a) is Return Address with a dedicated channel per request (correlation implicit in the channel name). Option (b) is Return Address with a shared per-instance channel plus Correlation Identifier demuxed by the waiter map.

## 2. How real systems route the reply

### NATS: ephemeral inbox subjects, muxed per connection

The canonical high-scale implementation of option (b)'s shape. "The client invents a fresh, unique subject to receive the answer on... It then publishes the request, and includes that reply subject as a field on the message," generated as `_INBOX.<random>`. Crucially, clients do not subscribe per request: "On its first request it subscribes once to a wildcard that stays fixed for the connection - a subject shaped like `_INBOX.<connection>.*`... thousands of concurrent requests cost one subscription, not thousands: every reply arrives on the same wildcard, and its final token identifies which request it belongs to." Every request carries a deadline, and the server sends a "no responders" 503 immediately when a subject has zero subscribers. **[vendor]** https://docs.nats.io/learn/core-nats/request-reply

Note NATS core is at-most-once: the muxed-inbox design accepts fire-and-forget reply loss and compensates with mandatory timeouts and fast failure signals.

### RabbitMQ: Direct Reply-To vs per-request queues

RabbitMQ documents both options and explains why it built the shared one. Per-request reply queues are the naive recipe, and the docs reject them on cost: "even an unreplied queue is relatively expensive to create and delete compared to the cost of receiving a reply." Direct Reply-To instead has the requestor consume from the pseudo-queue `amq.rabbitmq.reply-to`; the broker rewrites the name with an opaque per-consumer suffix and "the responder's ... channel process delivers the reply directly to the requester" with no real queue. The tradeoff is explicit: "Replies sent via Direct Reply-To are not fault-tolerant... RabbitMQ drops replies when" the requester disconnects; auto-ack only, no persistence. **[vendor]** https://www.rabbitmq.com/docs/direct-reply-to

So RabbitMQ's engineering position: per-request durable channels are too expensive to create, and the fix trades durability for a connection-scoped ephemeral channel. Redis lists invert the cost premise (an `LPUSH` creates a list for free), which weakens the analogy that per-request reply lists are the "expensive" option.

### Kafka: per-instance reply topics or partitions (Spring `ReplyingKafkaTemplate`)

Spring documents three deployment shapes for routing a reply to the right requestor instance, using `CORRELATION_ID`, `REPLY_TOPIC`, and optional `REPLY_PARTITION` headers:

1. Shared reply topic, each instance with a distinct `group.id`: "all instances receive each reply, but only the instance that sent the request finds the correlation ID"; useful for auto-scaling "but with the overhead of additional network traffic and the small cost of discarding each unwanted reply."
2. Shared reply topic with a dedicated partition per instance via `REPLY_PARTITION`: "The server must use this header to route the reply to the correct partition."
3. Otherwise "each instance needs a dedicated reply topic."

**[vendor]** https://docs.spring.io/spring-kafka/reference/kafka/sending-messages.html (section "Using `ReplyingKafkaTemplate`")

This is the strongest precedent for option (b): a static per-instance reply destination plus an in-process correlation map is the documented way to do request-reply on a log-structured broker where per-request destinations are infeasible.

### GCP Pub/Sub: no request-reply recipe, and hostile numbers for a 5s budget

Google does not document a request-reply pattern for Pub/Sub. Its own positioning doc frames Pub/Sub as implicit invocation where "Pub/Sub gives publishers no control over the delivery of the messages save for the guarantee of delivery," versus Cloud Tasks for explicit invocation. **[vendor]** https://docs.cloud.google.com/pubsub/docs/choosing-pubsub-or-cloud-tasks

Latency: the architecture doc names delivery latency a key metric but publishes no numbers and there is no latency SLA (the SLA covers publish availability only). **[vendor]** https://docs.cloud.google.com/pubsub/architecture, https://cloud.google.com/pubsub/sla A Google staff answer on the public forum states end-to-end delivery "is less than 200ms in most of the cases" but that "Pub/Sub has tail latency that can be significantly longer than 1s." **[community]** https://groups.google.com/g/cloud-pubsub-discuss/c/KH4gAqZRNNk

Redelivery: default is immediate redelivery on nack/deadline expiry; if a retry policy is configured, minimum backoff defaults to 10 seconds (configurable 0-600s) and "exponential backoff is only applied per-message." **[vendor]** https://docs.cloud.google.com/pubsub/docs/subscription-retry-policy A 10s minimum backoff exceeds the whole 5s scan budget, so any nacked request message is effectively a failed gate; the request leg must be treated as one-shot within the deadline, with the fallback decision (fail-open/fail-closed) owned by the waiter timeout, not by redelivery.

### Redis-based RPC in production systems

- **Celery `rpc://` backend**: closest production analog to the whole hybrid. Tasks travel over the broker; results come back as messages on a per-client reply queue ("uses reply-to and one queue per client", an anonymous exchange routed by a per-client UUID routing key). Documented consequences: "a result can only be retrieved once, and only by the client that initiated the task"; "Two different processes can't wait for the same result"; messages "are transient (non-persistent) by default." **[vendor]** https://docs.celeryq.dev/en/stable/userguide/tasks.html#rpc-result-backend, https://docs.celeryq.dev/en/latest/_modules/celery/backends/rpc.html Note this is per-client (per-instance), not per-request: it is shape (b) with correlation ids, on AMQP rather than Redis.
- **BullMQ**: `job.waitUntilFinished(queueEvents)` blocks a producer on a job result. The `QueueEvents` class "is implemented using Redis streams" and "requires a dedicated redis connection"; one QueueEvents instance per process demuxes completion events for all waiting callers. **[vendor]** https://api.docs.bullmq.io/classes/v5.QueueEvents.html, https://docs.bullmq.io/guide/events/ Notable: BullMQ deliberately chose Streams over Redis pub/sub for the reply/event leg, gaining persistence and catch-up reads, and still uses the shape-(b) topology (one shared per-process consumer plus in-memory demux).
- **Redis's own guidance**: the `BLPOP` page documents blocking list ops as the intended building block for event notification and queues, including the caveat that a popped element "only exists in the context of the client: if the client crashes while processing the returned element, it is lost forever" (acceptable here: the waiter that popped it is the only consumer that wants it). Multiple blocked clients on one key are served longest-waiting-first. **[vendor]** https://redis.io/docs/latest/commands/blpop/

### Temporal signals/updates as the reply channel

Temporal Updates are "a synchronous, blocking call that can change Workflow state ... and return a result," but the docs hedge on latency: "A Client sending an Update must wait until the Server delivers the Update to a Worker. Workers must be available and responsive," and they recommend Signals "if you need a response as soon as the Server receives the request." **[vendor]** https://docs.temporal.io/develop/go/message-passing The path is client -> Temporal server -> worker poll -> workflow task -> reply, several persisted hops. No first-party material was found positioning update-with-wait for sub-second request-reply at scale, and nothing credible surfaced from the community either. Given we already run Temporal this is tempting operationally, but there is no established recipe for it as a ~5s-budget inline gate; treat it as unproven for this use.

## 3. Documented pitfalls relevant to the two Redis options

- **Redis pub/sub is at-most-once, vendor-documented**: "Redis' Pub/Sub exhibits at-most-once message delivery semantics... If the subscriber is unable to handle the message (for example, due to an error or a network disconnect) the message is forever lost. If your application requires stronger delivery guarantees, you may want to learn about Redis Streams." **[vendor]** https://redis.io/docs/latest/develop/pubsub/#delivery-semantics For option (b), any subscriber reconnect window (deploy, Redis failover, transient network error) silently drops every in-flight reply for that replica; each becomes a 5s timeout with no way to distinguish "scanner slow" from "reply lost."
- **Blocking commands pin connections**: Redis's client guidance is explicit that multiplexing "can't support the blocking 'pop' commands (such as BLPOP) since these would stall the connection for all callers"; go-redis is a pooled client, so each concurrent `BLPOP` holds one pool connection for the full wait. **[vendor]** https://redis.io/docs/latest/develop/clients/pools-and-muxing/ Concurrency of option (a) is therefore bounded by `PoolSize` (and Redis `maxclients`), and slow scans can starve the pool for unrelated Redis traffic unless waiters use a separate client/pool.
- **go-redis blocking-command sharp edges** **[community, issue tracker]**: go-redis sets the read deadline to the block timeout plus a margin, and Redis only guarantees blocking commands return _no sooner_ than requested; on loaded servers they can return late enough to hit the read deadline, in which case the popped element is consumed server-side but dropped client-side (observed data loss with 1s BLPop). https://github.com/redis/go-redis/issues/647, https://github.com/redis/go-redis/issues/697 Also: pool exhaustion reports when many connections sit in blocking calls (https://github.com/redis/go-redis/issues/1261), and guidance not to disable client timeouts even when using context deadlines. Mitigations are known (block in short slices and loop, or generous ReadTimeout margin, dedicated pool) but this class of bug is real and Redis-load-dependent.
- **Orphaned replies**: with lists, a reply that arrives after the waiter timed out sits in the key; `EXPIRE`/TTL on the reply key is the standard GC and is trivial (the scanner sets a TTL at push, e.g. `LPUSH` + `PEXPIRE` in a pipeline or Lua). With pub/sub, orphans cost nothing (unmatched correlation ids are dropped by the waiter map), which Spring Kafka also normalizes ("the small cost of discarding each unwanted reply"). EIP does not prescribe orphan handling; every implementation handles it locally.
- **Pub/Sub request-leg backoff floor**: see section 2; a retry-policy minimum backoff (default 10s) is larger than the scan deadline, so redelivery can never rescue a scan, only pollute scanners with stale work. Scanner handlers should ack even on processing failure inside the deadline window and let the waiter timeout decide, or check a deadline stamp and drop stale messages.

## 4. What latency-sensitive blocking gates do in practice

First-party pattern evidence here is thin; treat as directional. AI gateway products that enforce guardrails as a blocking gate (Cloudflare AI Gateway with Llama Guard, agentgateway prompt guards) run the check inline in the request path via direct RPC to the moderation model, not via a broker round trip; the async/queued variants are explicitly the non-blocking "log and sample" modes, with sampling and timeout controls so "guardrail latency never blocks production traffic." **[community/secondary]** https://agentgateway.dev/docs/standalone/main/llm/prompt-guards/overview/, https://www.getmaxim.ai/articles/top-5-ai-gateways-for-implementing-guardrails-in-ai-applications/ No credible first-party writeup was found of a production blocking gate built on a cloud pub/sub fan-out with a side-channel reply; the established broker-based request-reply recipes (NATS, RabbitMQ RPC, Spring Kafka) all keep both legs on one broker whose delivery latency they control.

Implication: the hybrid is defensible (it reuses our existing fan-out and scanner topology), but the Pub/Sub request leg's undocumented tail latency is the budget risk that no reply-leg choice can fix, and the timeout-plus-default-decision path must be first-class.

## 5. Conclusion

**What the hybrid resembles.** "Request over broker, reply over a cheap per-caller side channel with correlation" is exactly Celery's `rpc://` backend and, structurally, EIP Return Address + Correlation Identifier. Within that family:

- Option (b) (per-replica channel + waiter map) is the same topology as NATS muxed inboxes, Spring Kafka's shared-reply-topic-with-per-instance-partition, RabbitMQ Direct Reply-To, and BullMQ QueueEvents: one static reply destination per instance, one subscriber, in-process demux. This is the shape every high-scale system converged on.
- Option (a) (list per request) is the "reply queue per request" recipe that RabbitMQ built Direct Reply-To to avoid, but the cost argument does not transfer: an AMQP queue is an expensive broker object, a Redis list key materializes for free on `LPUSH` and dies on `BLPOP` or TTL.

**BLPOP list per request vs channel per replica:**

| Axis                    | (a) BLPOP list per scan                                                                                                                            | (b) channel per replica + waiter map                                                                                                                                                     |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Loss semantics          | Reply is written to a key; survives waiter-not-yet-listening races (push-then-BLPOP returns immediately) and brief waiter delays; TTL GCs orphans. | At-most-once, vendor-documented "forever lost" on any subscriber gap; every reconnect window converts in-flight replies to timeouts.                                                     |
| Connection math         | One pooled connection held per concurrent waiting scan; bounded by pool size, needs a dedicated pool and ReadTimeout margin (go-redis #647/#697).  | One dedicated pub/sub connection per replica, O(1) regardless of concurrency.                                                                                                            |
| Lifecycle complexity    | Near zero: key name is the correlation id, no registration, no demux, scanner does `LPUSH`+`PEXPIRE`.                                              | Waiter-map registration/deregistration, subscriber health monitoring, reconnect handling, replica identity in the return address, and a decision for replies addressed to dead replicas. |
| Wrong-instance delivery | Impossible; only the creator knows the key.                                                                                                        | Prevented by per-replica channel naming, but a reply to a restarted/renamed replica is silently dropped.                                                                                 |

**Verdict for the design review.** At our expected concurrency (tens, not tens of thousands, of simultaneous blocking scans per replica) the connection cost that motivated the muxed-channel pattern at NATS/RabbitMQ scale does not bind, while the failure mode that pattern accepts (fire-and-forget reply loss) directly attacks a hard 5s gate where a lost reply is indistinguishable from a slow scanner. The vendor-documented at-most-once semantics of Redis pub/sub, versus the persisted, race-free, TTL-GCed semantics of a list, is the decisive evidence: prefer (a) BLPOP list per scan, with a dedicated go-redis pool for waiters, ReadTimeout comfortably above the block timeout, and TTL on reply keys. Revisit (b), or Redis Streams as BullMQ chose, only if concurrent-waiter counts approach pool/`maxclients` limits.

## Source index

- EIP Request-Reply / Return Address / Correlation Identifier: https://www.enterpriseintegrationpatterns.com/patterns/messaging/RequestReply.html, .../ReturnAddress.html, .../CorrelationIdentifier.html
- NATS request-reply and muxed inboxes: https://docs.nats.io/learn/core-nats/request-reply
- RabbitMQ Direct Reply-To: https://www.rabbitmq.com/docs/direct-reply-to
- Spring Kafka ReplyingKafkaTemplate: https://docs.spring.io/spring-kafka/reference/kafka/sending-messages.html
- GCP Pub/Sub retry policy: https://docs.cloud.google.com/pubsub/docs/subscription-retry-policy; architecture: https://docs.cloud.google.com/pubsub/architecture; SLA: https://cloud.google.com/pubsub/sla; vs Cloud Tasks: https://docs.cloud.google.com/pubsub/docs/choosing-pubsub-or-cloud-tasks; latency forum answer: https://groups.google.com/g/cloud-pubsub-discuss/c/KH4gAqZRNNk
- Redis BLPOP: https://redis.io/docs/latest/commands/blpop/; pub/sub semantics: https://redis.io/docs/latest/develop/pubsub/; pools and muxing: https://redis.io/docs/latest/develop/clients/pools-and-muxing/
- go-redis blocking issues: https://github.com/redis/go-redis/issues/647, https://github.com/redis/go-redis/issues/697, https://github.com/redis/go-redis/issues/1261
- Celery rpc:// backend: https://docs.celeryq.dev/en/stable/userguide/tasks.html#rpc-result-backend, https://docs.celeryq.dev/en/latest/_modules/celery/backends/rpc.html
- BullMQ QueueEvents/waitUntilFinished: https://api.docs.bullmq.io/classes/v5.QueueEvents.html, https://docs.bullmq.io/guide/events/
- Temporal message passing (updates): https://docs.temporal.io/develop/go/message-passing
- AI gateway guardrails (secondary): https://agentgateway.dev/docs/standalone/main/llm/prompt-guards/overview/

## Addendum: implemented topology (2026-08-20)

Further design discussion selected the replica-inbox topology, option (b) in shape, but backed by a Redis list instead of pub/sub. One drainer per replica provides O(1) connections and immediate waiter wakeup while retaining list persistence, the property that ruled out Redis pub/sub.

The lifecycle complexity scored against option (b) was accepted knowingly. Process restarts may drop in-flight replies because the corresponding HTTP requests and in-process waiters die with that process. The BLPOP-per-scan design remains the documented fallback if replica-inbox lifecycle or throughput behavior proves unsuitable.

## Addendum: waiter-aware polling replaces BLPOP (2026-08-26)

Review discussion moved the drainer from BLPOP wakeups to non-blocking LPOP polling gated on registered waiters. Blocking commands carried the design's ugliest client semantics: go-redis v9 ignores the configured read timeout for them, applies an internal block-plus-ten-seconds socket deadline, and a popped element can be lost when that deadline races its arrival (issues 647 and 697 above). Ordinary LPOPs keep normal timeout and pool-reconnect behavior, and a timed-out poll leaves elements in the list, so the dedicated-client replacement machinery was deleted outright.

The drainer sleeps on an in-process wake signal while no scans are in flight, so an idle replica issues zero Redis commands, cheaper than BLPOP's once-per-second reissue. The cost is up to one poll interval (default 25 ms) of added reply pickup latency, noise against the enforcement deadline. Single-key BLPOP carried no cluster constraint (it is single-slot by construction), so the switch was motivated by client semantics and simplicity, not cluster compatibility.
