# Ring of the Middle Earth

A browser-based, turn-based strategy game backed by a distributed system.

> **Hoca sunumu / PDF rapor (oyun + mimari birleşik):** [RAPOR_README.md](./RAPOR_README.md) — 18 bölümlük içindekiler, tam mimari (`ARCHITECTURE.md` dahil), reflection, rubrik, **28 görsel** rehberi.  
> Kısa mimari özet: [ARCHITECTURE.md](./ARCHITECTURE.md)

## Technology Choice: Option B — Go + Kafka

This implementation uses Go goroutines and Kafka KTable state stores as the distributed game engine.

## Architecture Summary

- **3 Go instances** behind an Nginx load balancer
- **Kafka** (3-broker cluster) as the event backbone and state store
- **Confluent Schema Registry** for Avro schema evolution
- **Vanilla JS + SSE** browser UI — no React/Vue/Angular

## Game timing (per project spec)

| Setting | Value |
|---------|--------|
| Turn duration | 60 seconds |
| Maximum turns | 40 (draw if no winner) |
| Detection hidden | Turns 1–3 |

Typical game length: about **15–25 minutes**; maximum **40 minutes** before a draw.

## Quick Start

```bash
make up
```

- **Light Side browser:** http://localhost?playerId=light  
- **Dark Side browser:** http://localhost?playerId=dark
- **Kafka broker:** localhost:29092
- **Schema Registry:** http://localhost:8081

## Run Tests (no Docker required)

```bash
make test
```

## Demo Scenarios

### Scenario 1 — Information Hiding
Move Ring Bearer to `weathertop`. Move Witch-King to `bree`.
Both browsers shown side by side — Dark Side receives `RING_BEARER_DETECTED`,
Light Side does not. `GET /game/state?playerId=dark` returns `ring-bearer.region=""`.

### Scenario 2 — Maia Dispatch
Submit `MAIA_ABILITY` for Gandalf on a BLOCKED path → TEMPORARILY_OPEN (blue).
Submit same order type for Saruman on `fords-of-isen-to-edoras` → PathCorrupted (permanent).

### Scenario 3 — Fault Tolerance
```bash
make kill-go2   # docker stop go-2
# Observe consumer group rebalance in logs
make start-go2  # docker start go-2
# go-2 recovers state from Kafka
```

## Project Structure

```
ring-of-the-middle-earth/
├── docker-compose.yml      ← Full system
├── Makefile                ← make up / make test
├── README.md               ← This file
├── nginx.conf              ← Load balancer config
├── config/
│   ├── units.conf          ← 14 units (JSON)
│   └── map.conf            ← 22 regions + 37 paths (JSON)
├── kafka/
│   └── schemas/            ← 13 Avro .avsc files
├── option-b/
│   ├── go.mod
│   ├── Dockerfile
│   ├── cmd/server/main.go  ← Entrypoint with 7-case select loop
│   ├── internal/
│   │   ├── config/         ← Config loader
│   │   ├── game/           ← Combat, detection, 13-step turn processing
│   │   ├── graph/          ← BFS + Dijkstra
│   │   ├── kafka/          ← Producer/consumer/topics
│   │   ├── router/         ← EventRouter (information hiding)
│   │   ├── pipeline/       ← Pipeline 1 (route risk) + Pipeline 2 (interception)
│   │   ├── cache/          ← WorldStateCache
│   │   └── api/            ← HTTP handlers + SSE
│   └── tests/              ← Unit tests
└── ui/
    ├── index.html          ← Game UI
    ├── game.js             ← SSE client + game logic
    └── style.css           ← Styling
```
