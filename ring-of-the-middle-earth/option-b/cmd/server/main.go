package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/rotr/option-b/internal/api"
	"github.com/rotr/option-b/internal/cache"
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/game"
	kafkaclient "github.com/rotr/option-b/internal/kafka"
	"github.com/rotr/option-b/internal/router"
)

func main() {
	brokers := envOrDefault("KAFKA_BROKERS", "localhost:9092")
	unitsPath := envOrDefault("UNITS_CONFIG", "../../config/units.conf")
	mapPath := envOrDefault("MAP_CONFIG", "../../config/map.conf")
	port := envOrDefault("PORT", "8080")

	cfg, err := config.Load(unitsPath, mapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	g := game.BuildGraph(cfg)

	pathCfgMap := make(map[string][2]string)
	for id, pc := range cfg.Paths {
		pathCfgMap[id] = [2]string{pc.From, pc.To}
	}

	// Topics are created by the kafka-init container; skip here to avoid ARM64 AdminClient crash
	// kafkaclient.CreateTopics(brokers)

	// Wait for Kafka to be ready
	waitForKafka(brokers)

	producer, err := kafkaclient.NewProducer(brokers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create kafka producer: %v\n", err)
		os.Exit(1)
	}
	defer producer.Close()

	worldCache := cache.NewWorldStateCache(cfg.Units)
	gs := game.InitState(cfg)
	eventRouter := router.NewEventRouter()
	var ordersMu sync.Mutex
	handler := api.NewHandler(worldCache, producer, cfg, g, pathCfgMap, gs, &ordersMu)

	r := mux.NewRouter()
	r.HandleFunc("/game/start", handler.StartGame).Methods("POST")
	r.HandleFunc("/order", handler.SubmitOrder).Methods("POST")
	r.HandleFunc("/game/state", handler.GetGameState).Methods("GET")
	r.HandleFunc("/orders/available", handler.GetAvailableOrders).Methods("GET")
	r.HandleFunc("/analysis/routes", handler.GetRouteAnalysis).Methods("GET")
	r.HandleFunc("/analysis/intercept", handler.GetInterceptAnalysis).Methods("GET")
	r.HandleFunc("/events", handler.SSEEvents).Methods("GET")
	r.HandleFunc("/health", handler.GetHealth).Methods("GET")

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if req.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	topics := []string{
		kafkaclient.TopicOrdersValidated,
		kafkaclient.TopicBroadcast,
		kafkaclient.TopicEventsUnit,
		kafkaclient.TopicEventsRegion,
		kafkaclient.TopicEventsPath,
		kafkaclient.TopicRingPosition,
		kafkaclient.TopicRingDetection,
	}
	consumer, err := kafkaclient.NewConsumer(brokers, "game-engine-group", topics)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create kafka consumer: %v\n", err)
		os.Exit(1)
	}
	defer consumer.Close()

	rawConsumer, err := kafkaclient.NewConsumer(brokers, "order-validator-group", []string{kafkaclient.TopicOrdersRaw})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create order validator consumer: %v\n", err)
		os.Exit(1)
	}
	defer rawConsumer.Close()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	newConnectionCh := make(chan string, 10)
	disconnectCh := make(chan string, 10)
	analysisRequestCh := make(chan string, 10)
	cacheUpdateCh := eventRouter.CacheUpdateCh
	kafkaConsumerCh := make(chan *kafkaclient.Message, 100)
	engineCh := eventRouter.EngineCh

	// Kafka consumer goroutine
	go func() {
		for {
			msg, err := consumer.Poll(100)
			if err != nil {
				fmt.Fprintf(os.Stderr, "consumer poll error: %v\n", err)
				time.Sleep(time.Second)
				continue
			}
			if msg != nil {
				kafkaConsumerCh <- msg
			}
		}
	}()

	// Order validation goroutine
	go func() {
		for {
			msg, err := rawConsumer.Poll(100)
			if err != nil || msg == nil {
				continue
			}
			var order game.Order
			if err := json.Unmarshal(msg.Value, &order); err != nil {
				continue
			}
			playerSide := game.SideFreePeoples
			if order.PlayerID == "dark" || order.PlayerID == "shadow" || order.PlayerID == "player2" {
				playerSide = game.SideShadow
			}
			validationErr := game.ValidateOrder(&order, gs, cfg, playerSide)
			if validationErr != nil {
				dlqMsg := map[string]interface{}{
					"originalTopic": kafkaclient.TopicOrdersRaw,
					"errorCode":     validationErr.Code,
					"errorMessage":  validationErr.Error(),
					"rawPayload":    msg.Value,
					"timestamp":     time.Now().UnixMilli(),
				}
				_ = producer.Produce(kafkaclient.TopicDLQ, validationErr.Code, dlqMsg)
				continue
			}
			_ = producer.Produce(kafkaclient.TopicOrdersValidated, order.UnitID, order)
		}
	}()

	// engineCh is kept for future distributed use; drain it
	go func() {
		for range engineCh {
		}
	}()

	// Turn timer goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.TurnDurationSec) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ordersMu.Lock()
			fmt.Printf("--- processing turn %d (orders: %d, rb=%s, route=%d paths) ---\n",
				gs.Turn, len(gs.Orders), gs.RingBearer.TrueRegion, len(gs.RingBearer.Route))
			events := game.ProcessTurn(gs, cfg, g)
			fmt.Printf("--- turn done: %d events, new turn=%d, rb=%s ---\n",
				len(events), gs.Turn, gs.RingBearer.TrueRegion)
			ordersMu.Unlock()

			var regions []*game.RegionState
			for _, region := range gs.Regions {
				rCopy := *region
				regions = append(regions, &rCopy)
			}
			var units []*game.UnitSnapshot
			for _, u := range gs.Units {
				uCopy := *u
				units = append(units, &uCopy)
			}
			snap := game.WorldStateSnapshot{
				Turn:      gs.Turn,
				Regions:   regions,
				Units:     units,
				Status:    gs.Status,
				Winner:    gs.Winner,
				Timestamp: time.Now().UnixMilli(),
			}
			worldCache.Update(snap, gs.RingBearer.TrueRegion, gs.RingBearer.Route, gs.RingBearer.RouteIdx)

			// Build SSE world-state payload (strip RB region for dark side)
			lightPayload, _ := json.Marshal(map[string]interface{}{
				"type":   "world_state",
				"turn":   snap.Turn,
				"status": snap.Status,
				"winner": snap.Winner,
			})
			handler.BroadcastToLight(lightPayload)
			handler.BroadcastToDark(lightPayload)

			// Also produce to Kafka for observability (best-effort)
			_ = producer.Produce(kafkaclient.TopicBroadcast, "world", snap)

			for _, ev := range events {
				evBytes, _ := json.Marshal(map[string]interface{}{
					"type":    ev.Type,
					"payload": ev.Payload,
					"turn":    gs.Turn,
				})
				switch ev.Type {
				case "RingBearerMoved":
					rbMsg := map[string]interface{}{
						"type":       "RingBearerMoved",
						"trueRegion": ev.Payload,
						"turn":       gs.Turn,
					}
					rbBytes, _ := json.Marshal(rbMsg)
					handler.BroadcastToLight(rbBytes)
					_ = producer.Produce(kafkaclient.TopicRingPosition, "ring-bearer", rbMsg)
					fmt.Printf("  RING BEARER MOVED → %v (turn %d)\n", ev.Payload, gs.Turn)

				case "RingBearerDetected":
					detMsg := map[string]interface{}{
						"type":     "RingBearerDetected",
						"regionId": ev.Payload,
						"turn":     gs.Turn,
					}
					detBytes, _ := json.Marshal(detMsg)
					handler.BroadcastToDark(detBytes)
					worldCache.UpdateDetection(fmt.Sprintf("%v", ev.Payload), gs.Turn)
					_ = producer.Produce(kafkaclient.TopicRingDetection, "shadow", detMsg)
					fmt.Printf("  RING BEARER DETECTED at %v (turn %d)\n", ev.Payload, gs.Turn)

				case "GameOver":
					if goe, ok := ev.Payload.(game.GameOverEvent); ok {
						goeBytes, _ := json.Marshal(map[string]interface{}{
							"type":   "game_over",
							"winner": goe.Winner,
							"cause":  goe.Cause,
							"turn":   goe.Turn,
						})
						handler.BroadcastToLight(goeBytes)
						handler.BroadcastToDark(goeBytes)
						fmt.Printf("  *** GAME OVER: winner=%s cause=%s turn=%d ***\n", goe.Winner, goe.Cause, goe.Turn)
					}

				case "PathBlocked", "PathOpened", "PathCorrupted", "RouteBlocked":
					handler.BroadcastToLight(evBytes)
					handler.BroadcastToDark(evBytes)
					_ = producer.Produce(kafkaclient.TopicEventsPath, fmt.Sprintf("%v", ev.Payload), ev)

				case "UnitMoved", "UnitRespawned", "NazgulDeployed":
					handler.BroadcastToLight(evBytes)
					handler.BroadcastToDark(evBytes)
					_ = producer.Produce(kafkaclient.TopicEventsUnit, fmt.Sprintf("%v", ev.Payload), ev)

				case "BattleResolved", "IsengardDestroyed":
					handler.BroadcastToLight(evBytes)
					handler.BroadcastToDark(evBytes)
					_ = producer.Produce(kafkaclient.TopicEventsRegion, fmt.Sprintf("%v", ev.Payload), ev)

				default:
					handler.BroadcastToLight(evBytes)
					handler.BroadcastToDark(evBytes)
				}
			}
			producer.Flush(500)
		}
	}()

	// Main select loop — all 7 required cases per Section 31
	go func() {
		for {
			select {
			case msg := <-kafkaConsumerCh:
				eventRouter.Route(msg)

			case conn := <-newConnectionCh:
				fmt.Printf("new connection: %s\n", conn)

			case disc := <-disconnectCh:
				fmt.Printf("disconnect: %s\n", disc)

			case req := <-analysisRequestCh:
				fmt.Printf("analysis request: %s\n", req)

			case snap := <-cacheUpdateCh:
				_ = snap

			case <-time.After(60 * time.Second):
				fmt.Printf("heartbeat: turn=%d\n", gs.Turn)

			case sig := <-signalCh:
				fmt.Printf("received signal: %v, shutting down\n", sig)
				producer.Flush(5000)
				os.Exit(0)
			}
		}
	}()

	// SSE broadcast goroutine
	go func() {
		for {
			select {
			case ev := <-eventRouter.LightSideSSECh:
				handler.BroadcastToLight(ev.Payload)
			case ev := <-eventRouter.DarkSideSSECh:
				handler.BroadcastToDark(ev.Payload)
			}
		}
	}()

	srv := &http.Server{
		Addr:        ":" + port,
		Handler:     r,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	fmt.Printf("Ring of the Middle Earth server starting on :%s\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// waitForKafka retries connecting to Kafka until it's ready
func waitForKafka(brokers string) {
	fmt.Printf("Waiting for Kafka at %s...\n", brokers)
	for i := 0; i < 30; i++ {
		p, err := kafkaclient.NewProducer(brokers)
		if err == nil {
			p.Close()
			fmt.Println("Kafka is ready.")
			return
		}
		fmt.Printf("Kafka not ready (attempt %d/30): %v\n", i+1, err)
		time.Sleep(3 * time.Second)
	}
	fmt.Fprintln(os.Stderr, "Kafka did not become ready in time, continuing anyway...")
}
