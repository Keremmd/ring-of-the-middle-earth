# Ring of the Middle Earth — Architecture Document
## Option B: Go + Kafka

---

## 1. System Diagram

```
┌────────────────────────────────────────────────────────────────────┐
│                        BROWSER LAYER                               │
│                                                                    │
│  Browser A (Light Side)              Browser B (Dark Side)         │
│  POST /order                         POST /order                   │
│  GET  /events (SSE)                  GET  /events (SSE)            │
│  GET  /orders/available              GET  /orders/available        │
│  GET  /analysis/routes               GET  /analysis/intercept      │
│  GET  /game/state                    GET  /game/state              │
└────────────────┬─────────────────────────────┬─────────────────────┘
                 │ HTTP/SSE                     │ HTTP/SSE
                 ▼                             ▼
┌───────────────────────────────────────────────────────────────────┐
│                    NGINX LOAD BALANCER (:80)                      │
│          Round-robin across go-1, go-2, go-3                      │
└─────────────┬───────────────┬───────────────┬──────────────────────┘
              │               │               │
              ▼               ▼               ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│  go-1    │ │  go-2    │ │  go-3    │  ← 3 stateless Go instances
│  :8080   │ │  :8082   │ │  :8083   │    (one Kafka consumer group)
└────┬─────┘ └────┬─────┘ └────┬─────┘
     │             │             │
     └─────────────┴─────────────┘
                   │ Produces/Consumes
                   ▼
┌──────────────────────────────────────────────────────────────────┐
│                    KAFKA CLUSTER (3 brokers)                      │
│                                                                   │
│  10 Topics:                                                       │
│  ┌─────────────────────┬──────┬──────┬─────────┬──────────────┐  │
│  │ Topic               │Parts │Replic│Cleanup  │Retention     │  │
│  ├─────────────────────┼──────┼──────┼─────────┼──────────────┤  │
│  │ game.orders.raw     │  3   │  3   │ delete  │ 1h           │  │
│  │ game.orders.validated│ 6   │  3   │ delete  │ 1h           │  │
│  │ game.events.unit    │  6   │  3   │ delete  │ 7d           │  │
│  │ game.events.region  │  6   │  3   │ delete  │ 7d           │  │
│  │ game.events.path    │  6   │  3   │ delete  │ 7d           │  │
│  │ game.session        │  1   │  3   │ compact │ ∞            │  │
│  │ game.broadcast      │  1   │  3   │ delete  │ 1h           │  │
│  │ game.ring.position  │  1   │  3   │ delete  │ 1h           │  │
│  │ game.ring.detection │  2   │  3   │ delete  │ 1h           │  │
│  │ game.dlq            │  3   │  3   │ delete  │ 7d           │  │
│  └─────────────────────┴──────┴──────┴─────────┴──────────────┘  │
│                                                                   │
│  Confluent Schema Registry (:8081)                                │
│  ← All Avro schemas registered here                               │
└──────────────────────────────────────────────────────────────────┘
```

---

## 2. Goroutine Architecture

```
main()
 │
 ├── KafkaConsumer goroutine
 │     Polls game.orders.validated, game.broadcast,
 │     game.events.*, game.ring.position, game.ring.detection
 │     → kafkaConsumerCh (buffered, cap=100)
 │
 ├── OrderValidator goroutine
 │     Polls game.orders.raw
 │     Runs 8 validation rules against KTables
 │     Valid → game.orders.validated
 │     Invalid → game.dlq
 │
 ├── TurnProcessor goroutine
 │     Reads engineCh (validated orders)
 │     Accumulates gs.Orders[unitID] = order
 │
 ├── TurnTimer goroutine
 │     time.Ticker(60s)
 │     Calls game.ProcessTurn(gs, cfg, g)  ← 13-step execution
 │     Produces WorldStateSnapshot → game.broadcast
 │     Produces events → game.events.*, game.ring.position,
 │                        game.ring.detection
 │
 ├── Pipeline1 (Route Risk) — 4 workers
 │     Dispatcher → workCh (buffered, cap=20) → 4 workers
 │     → resultCh → Aggregator → RankedRouteList
 │     Triggered by GET /analysis/routes
 │     Timeout: 2s, context.Context cancellation
 │     sync.WaitGroup at stage boundaries
 │
 ├── Pipeline2 (Interception) — 4 workers
 │     Dispatcher → workCh (buffered, cap=30) → 4 workers
 │     → resultCh → Aggregator → InterceptPlan
 │     Triggered by GET /analysis/intercept
 │     Timeout: 2s, context.Context cancellation
 │
 ├── SSE goroutines (one per connected player)
 │     LightSide: reads from lightSSECh
 │     DarkSide: reads from darkSSECh
 │
 ├── HTTP server goroutine
 │     mux.Router → handler functions
 │     All endpoints documented in Section 34
 │
 └── Main select loop (7 required cases)
       select {
       case msg  := <-kafkaConsumerCh:   → EventRouter.Route(msg)
       case conn := <-newConnectionCh:   → log
       case disc := <-disconnectCh:      → log
       case req  := <-analysisRequestCh: → log
       case snap := <-cacheUpdateCh:     → cache update
       case tick := <-time.After(60s):   → heartbeat
       case sig  := <-signalCh:          → graceful shutdown
       }
```

### Channel Summary

| Channel | Direction | Buffer | Purpose |
|---------|-----------|--------|---------|
| `kafkaConsumerCh` | Kafka→Router | 100 | All consumed Kafka messages |
| `LightSideSSECh` | Router→SSE | 100 | Light Side events |
| `DarkSideSSECh` | Router→SSE | 100 | Dark Side events (RB stripped) |
| `cacheUpdateCh` | Router→Cache | 100 | World state updates |
| `engineCh` | Router→Engine | 100 | Validated orders |
| Pipeline `workCh` | Dispatcher→Worker | 20/30 | Route/intercept computations |
| Pipeline `resultCh` | Worker→Aggregator | 0 (unbuffered) | Results collection |

---

## 3. Kafka Diagram

```
                    PRODUCERS                         CONSUMERS
                    ─────────                         ─────────

Browser             game.orders.raw  ──────►  OrderValidator goroutine
(POST /order)             │                        (group: order-validator)
                          │                              │
                          │                              ▼
                          │                   game.orders.validated
                          │                    ├── TurnProcessor goroutine
                          │                    └── Topology 2 (risk enrichment)
                          │
TurnTimer goroutine ──► game.broadcast        ──► EventRouter → SSE (both sides)
                     ──► game.ring.position   ──► EventRouter → LightSide SSE ONLY
                     ──► game.ring.detection  ──► EventRouter → DarkSide SSE ONLY
                     ──► game.events.unit     ──► EventRouter → both sides
                     ──► game.events.region   ──► EventRouter → both sides
                     ──► game.events.path     ──► EventRouter → both sides
                     ──► game.session         ──► TurnKTable (log-compacted)
                     ──► game.dlq             ──► Dead letter queue

Topology 1          ──► game.orders.validated (invalid → DLQ)
Topology 2          ──► game.orders.validated (enriched with routeRiskScore)
```

### Partition Key Rationale

| Topic | Partition Key | Rationale |
|-------|---------------|-----------|
| game.orders.raw | playerId | All orders from one player go to same partition for ordering |
| game.orders.validated | unitId | One partition per unit ensures per-unit ordering |
| game.events.unit | unitId | Unit events ordered per unit |
| game.events.region | regionId | Region events ordered per region |
| game.events.path | pathId | Path events ordered per path |
| game.ring.detection | playerId | Dark-side-only; 2 partitions per player side |
| game.dlq | errorCode | Group errors by type for easy monitoring |

---

## 4. Information Hiding: EventRouter

The `EventRouter` is the **single enforcement point** for Ring Bearer position secrecy.

```go
switch event.Topic {

case "game.ring.position":
    lightSideSSECh <- event          // Light Side ONLY
    // never darkSideSSECh

case "game.ring.detection":
    darkSideSSECh <- event           // Dark Side ONLY
    // never lightSideSSECh

case "game.broadcast":
    lightSideSSECh <- event          // Full snapshot
    darkSideSSECh <- stripRingBearer(event)  // RB.region = ""

case game.events.*:
    lightSideSSECh <- event
    darkSideSSECh  <- event
}
```

`DarkView.RingBearerRegion` is **always `""`**. This invariant is maintained by:
1. `router.go`: stripRingBearer() zeroes the field before delivery
2. `cache.go`: every `Update()` and `Snapshot()` call enforces `DarkView.RingBearerRegion = ""`
3. `router_test.go -race`: race detector verifies no data race can expose the true region

---

## 5. Config-Driven Design (No Hardcoded Unit IDs)

All unit behavior is driven by `UnitConfig`. The `id` field is **never referenced** in game logic.

```go
// WRONG (hardcoded):
if unitID == "witch-king" { effectiveRange += 1 }

// CORRECT (config-driven):
for id, uc := range cfg.Units {
    if uc.Maia && uc.Side == "SHADOW" && uc.StartRegion == "mordor" {
        // This is Sauron — by config, not by ID
        sauronBonus = 1
    }
}
```

Same for Maia dispatch — Gandalf and Saruman both receive `MaiaAbility` orders:

```go
// Dispatch by config.Side, not by config.ID
if uc.Side == "FREE_PEOPLES" {
    // Gandalf: OpenPath
} else if uc.Side == "SHADOW" {
    // Saruman: CorruptPath
}
```

---

## 6. Fault Tolerance

### Option B: Kafka Consumer Group Protocol

The 3 Go instances form a single Kafka consumer group (`game-engine-group`).

**Failure scenario:**
1. `docker stop go-2` during turn processing
2. Kafka detects failure (session.timeout.ms = 6000ms)
3. Kafka triggers **consumer group rebalance**
4. Partitions previously assigned to `go-2` are redistributed to `go-1` and `go-3`
5. Game continues uninterrupted — `go-1` and `go-3` have all state in KTables

**Recovery scenario:**
1. `docker start go-2`
2. `go-2` reconnects to Kafka consumer group
3. Kafka assigns partitions back to `go-2`
4. `go-2` replays events from Kafka and rebuilds its local KTable view
5. `go-2` rejoins fully recovered

### State Location

All authoritative game state lives in Kafka KTable state stores:

| KTable | Key | Purpose |
|--------|-----|---------|
| UnitKTable | unitId | Unit positions and status |
| RegionKTable | regionId | Region control and fortification |
| PathKTable | pathId | Path status and surveillance |
| RingBearerKTable | "ring-bearer" | True Ring Bearer position (never exposed) |

### Exactly-Once GameOver

`GameOver` is produced with `enable.idempotence=true`. Even if the Go instance crashes mid-transaction and restarts, the idempotent producer ensures the event appears **exactly once** in `game.broadcast`.

---

## 7. Paradigm Justification

### Why Go + Kafka is well-suited to this problem

1. **Goroutines map naturally to game subsystems.** The KafkaConsumer, TurnProcessor, EventRouter, and Pipeline workers are independent concerns that benefit from lightweight concurrency without the overhead of actor lifecycle management.

2. **Kafka as state store enables stateless application tier.** All 3 Go instances are identical and interchangeable. Any instance can handle any request. This is ideal for a game with infrequent state mutations (one turn per minute) but frequent reads (game state polling).

3. **Channel-based pipelines match the analysis workload.** The route risk and interception pipelines fan out across 4 workers and aggregate results — a pattern Go channels express cleanly.

4. **Information hiding is enforced at a single point.** The EventRouter is one function with a simple switch statement. No actor messaging or message passing abstraction to reason through — just channels and a clear routing table.

### What is genuinely harder with Go than with Akka

1. **State persistence and recovery.** Akka Persistence provides built-in event sourcing with journals and snapshots. In Go, we rely entirely on Kafka for state recovery — which works, but requires every state mutation to be a Kafka event. Managing KTable consistency across 3 instances during a rebalance requires careful offset management.

2. **Typed message dispatch.** Go's `interface{}` and type switches are less ergonomic than Akka's typed `Behavior[T]` system. The turn processor's switch on `orderType` strings is less safe than pattern matching against sealed trait hierarchies.

### How Akka would solve the hardest parts

1. **Turn processing atomicity:** `WorldStateActor` as a Cluster Singleton would own the entire mutable game state and process all orders sequentially in its mailbox — eliminating the need for mutex-protected shared state. The Akka model guarantees per-actor sequential message processing.

2. **Ring Bearer secrecy:** `RingBearerActor` as a Cluster Singleton holds `trueRegion` and never publishes it to shared topics. The actor boundary enforces the information asymmetry at the type level — no other actor can `ask` for the true region.

---

## 8. Reflection (minimum 300 words)

The most challenging aspect of this implementation was the **information asymmetry enforcement**. The requirement that `DarkView.RingBearerRegion` must be `""` at all times sounds simple but requires careful attention throughout the codebase. Every place that copies the game state — the `Snapshot()` method, the `Update()` method, the `stripRingBearer()` function in the EventRouter — must independently enforce this invariant. A single missing assignment anywhere in the chain would silently violate the spec.

The solution was to treat the `DarkView.RingBearerRegion = ""` assignment as an **invariant** rather than a best-effort practice. Every mutating function in `WorldStateCache` ends with an explicit assignment. The `router_test.go -race` tests verify this holds even under concurrent access. This defensive programming style is unusual for Go, which often relies on caller convention, but was necessary here.

The **13-step turn processing** was also harder than expected. The spec defines a strict ordering of side effects: routes before blocks, blocks before advances, advances before attacks, and so on. In Akka, each step would be a separate message to a dedicated actor, and the actor's mailbox would ensure ordering. In Go, we call these functions sequentially in a single goroutine, which is actually simpler — but required careful reading of the spec to ensure step dependencies were not violated. The interaction between step 3 (blocking) and step 7 (auto-advance) was particularly subtle: a path blocked in step 3 must suppress the advance in step 7 for any unit whose route includes that path.

The **config-driven dispatch** was intellectually satisfying. The requirement that no `unitId` string literal appears in game logic forced a more principled design. Instead of `if unitID == "gandalf"`, we check `if uc.Class == "Maia" && uc.Side == "FREE_PEOPLES"`. This makes the system genuinely extensible: adding a new Maia unit requires only a config entry.

If redesigning this system, I would use a **single-writer architecture** more aggressively. The current design has the TurnProcessor goroutine accumulate orders and the TurnTimer goroutine call `ProcessTurn()` — both accessing `gs.Orders`. This works because they use the same goroutine semantics, but a cleaner design would have a single `GameStateManager` goroutine own all mutable state and expose read-only snapshots to other goroutines via channels.

---

## 9. LLM Usage Log (Required Appendix)

| Interaction | Prompt | Used | Changed/Rejected |
|-------------|--------|------|-----------------|
| 1 | "Explain Kafka KTable semantics and how they relate to state recovery after consumer group rebalance" | Used to understand KTable reconciliation during rebalance | Simplified the explanation for the architecture doc |
| 2 | "How does enable.idempotence work in Kafka producers and what guarantees does it provide?" | Used to implement the exactly-once GameOver guarantee | Verified against Kafka 3.6 docs directly |
| 3 | "What are the tradeoffs between Akka Cluster Sharding and Kafka Consumer Group rebalancing for stateful distributed systems?" | Used as a starting point for Section 7 (Paradigm Justification) | Rewrote entirely in own words to reflect actual implementation choices |
| 4 | "How to implement a fan-out/fan-in pipeline in Go using goroutines and channels with context cancellation?" | Used for Pipeline 1 and 2 structure | Changed channel buffer sizes based on the spec requirements (cap=20, cap=30) |
| 5 | "Best practices for enforcing information hiding invariants in Go concurrent code" | Used for WorldStateCache design | Added explicit `DarkView.RingBearerRegion = ""` enforcement at every mutation point |
