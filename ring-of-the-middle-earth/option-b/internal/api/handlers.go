package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/rotr/option-b/internal/cache"
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/game"
	"github.com/rotr/option-b/internal/graph"
	"github.com/rotr/option-b/internal/kafka"
	"github.com/rotr/option-b/internal/pipeline"
)

// Handler holds all dependencies for HTTP handlers
type Handler struct {
	Cache      *cache.WorldStateCache
	Producer   *kafka.Producer
	GameCfg    *config.GameConfig
	Graph      *graph.Graph
	PathCfgMap map[string][2]string
	GameState  *game.GameState
	OrdersMu   *sync.Mutex
	// SSE client management
	mu           sync.RWMutex
	lightClients map[string]chan []byte
	darkClients  map[string]chan []byte
}

// NewHandler creates a new API handler
func NewHandler(
	c *cache.WorldStateCache,
	p *kafka.Producer,
	cfg *config.GameConfig,
	g *graph.Graph,
	pathCfgMap map[string][2]string,
	gs *game.GameState,
	ordersMu *sync.Mutex,
) *Handler {
	return &Handler{
		Cache:        c,
		Producer:     p,
		GameCfg:      cfg,
		Graph:        g,
		PathCfgMap:   pathCfgMap,
		GameState:    gs,
		OrdersMu:     ordersMu,
		lightClients: make(map[string]chan []byte),
		darkClients:  make(map[string]chan []byte),
	}
}

// StartGame handles POST /game/start — also resets the game if already finished
func (h *Handler) StartGame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Mode != "HVH" {
		http.Error(w, "only HVH mode supported", http.StatusBadRequest)
		return
	}

	// Reset game state for a fresh game
	h.OrdersMu.Lock()
	fresh := game.InitState(h.GameCfg)
	*h.GameState = *fresh
	h.OrdersMu.Unlock()

	// Also clear the world cache so /game/state immediately returns clean data
	h.Cache.Reset()

	sessionMsg := map[string]interface{}{
		"sessionId": fmt.Sprintf("game-%d", time.Now().UnixMilli()),
		"mode":      "HVH",
		"status":    "ACTIVE",
		"turn":      1,
		"timestamp": time.Now().UnixMilli(),
	}
	_ = h.Producer.Produce(kafka.TopicSession, "session", sessionMsg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sessionMsg)
}

// SubmitOrder handles POST /order — writes directly into game state (single-instance mode)
func (h *Handler) SubmitOrder(w http.ResponseWriter, r *http.Request) {
	var order game.Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Auto-set turn if not provided (or if mismatched) to allow easy API usage
	if order.Turn == 0 || order.Turn != h.GameState.Turn {
		order.Turn = h.GameState.Turn
	}

	playerSide := game.SideFreePeoples
	if order.PlayerID == "dark" || order.PlayerID == "shadow" || order.PlayerID == "player2" {
		playerSide = game.SideShadow
	}
	if validationErr := game.ValidateOrder(&order, h.GameState, h.GameCfg, playerSide); validationErr != nil {
		http.Error(w, validationErr.Error(), http.StatusBadRequest)
		return
	}

	h.OrdersMu.Lock()
	h.GameState.Orders[order.UnitID] = &order
	h.OrdersMu.Unlock()

	// Also produce to Kafka for observability
	_ = h.Producer.Produce(kafka.TopicOrdersRaw, order.PlayerID, order)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

// GetGameState handles GET /game/state
func (h *Handler) GetGameState(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("playerId")
	snap := h.Cache.Snapshot()

	type publicUnit struct {
		ID           string `json:"id"`
		Region       string `json:"region"`
		Strength     int    `json:"strength"`
		Status       string `json:"status"`
		RespawnTurns int    `json:"respawnTurns"`
	}

	var units []publicUnit
	for id, u := range snap.Units {
		region := u.Region
		// Strip Ring Bearer position for Dark Side
		if id == "ring-bearer" {
			cfg := h.GameCfg.Units[id]
			if cfg.Class == "RingBearer" {
				if !isLightSidePlayer(playerID) {
					region = "" // ENFORCED
				}
			}
		}
		units = append(units, publicUnit{
			ID:           id,
			Region:       region,
			Strength:     u.Strength,
			Status:       string(u.Status),
			RespawnTurns: u.RespawnTurns,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"turn":                 snap.Turn,
		"status":               snap.Status,
		"winner":               snap.Winner,
		"maxTurns":             h.GameCfg.MaxTurns,
		"turnDurationSeconds":  h.GameCfg.TurnDurationSec,
		"hiddenUntilTurn":      h.GameCfg.HiddenUntilTurn,
		"units":                units,
		"regions":              snap.Regions,
		"paths":                snap.Paths,
	})
}

// GetAvailableOrders handles GET /orders/available
func (h *Handler) GetAvailableOrders(w http.ResponseWriter, r *http.Request) {
	unitID := r.URL.Query().Get("unitId")
	playerID := r.URL.Query().Get("playerId")

	snap := h.Cache.Snapshot()
	u, ok := snap.Units[unitID]
	if !ok {
		http.Error(w, "unit not found", http.StatusNotFound)
		return
	}

	uc := h.GameCfg.Units[unitID]
	playerSide := game.SideFreePeoples
	if !isLightSidePlayer(playerID) {
		playerSide = game.SideShadow
	}
	if game.Side(uc.Side) != playerSide {
		http.Error(w, "not your unit", http.StatusForbidden)
		return
	}

	var orders []string
	if u.Status == game.StatusActive {
		orders = append(orders, "ASSIGN_ROUTE", "REDIRECT_UNIT")

		if uc.Maia {
			orders = append(orders, "MAIA_ABILITY")
		}
		if uc.CanFortify {
			orders = append(orders, "FORTIFY_REGION")
		}
		if uc.Side == "SHADOW" {
			orders = append(orders, "BLOCK_PATH", "SEARCH_PATH", "DEPLOY_NAZGUL")
		}
		if uc.Class == "RingBearer" {
			orders = append(orders, "DESTROY_RING")
		}
		orders = append(orders, "ATTACK_REGION", "REINFORCE_REGION")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"unitId": unitID,
		"orders": orders,
		"region": u.Region,
		"turn":   snap.Turn,
	})
}

// GetRouteAnalysis handles GET /analysis/routes (Light Side only)
func (h *Handler) GetRouteAnalysis(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("playerId")
	if !isLightSidePlayer(playerID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	snap := h.Cache.Snapshot()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	result := pipeline.RunRouteRiskPipeline(ctx, snap, h.Graph, h.PathCfgMap)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetInterceptAnalysis handles GET /analysis/intercept (Dark Side only)
func (h *Handler) GetInterceptAnalysis(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("playerId")
	if isLightSidePlayer(playerID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	snap := h.Cache.Snapshot()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	result := pipeline.RunInterceptionPipeline(ctx, snap, h.Graph, h.PathCfgMap)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GetHealth handles GET /health
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// SSEEvents handles GET /events
func (h *Handler) SSEEvents(w http.ResponseWriter, r *http.Request) {
	playerID := r.URL.Query().Get("playerId")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientCh := make(chan []byte, 50)
	h.mu.Lock()
	if isLightSidePlayer(playerID) {
		h.lightClients[playerID] = clientCh
	} else {
		h.darkClients[playerID] = clientCh
	}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if isLightSidePlayer(playerID) {
			delete(h.lightClients, playerID)
		} else {
			delete(h.darkClients, playerID)
		}
		h.mu.Unlock()
	}()

	for {
		select {
		case msg := <-clientCh:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// BroadcastToLight sends an event to all connected Light Side SSE clients
func (h *Handler) BroadcastToLight(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.lightClients {
		select {
		case ch <- data:
		default:
		}
	}
}

// BroadcastToDark sends an event to all connected Dark Side SSE clients
func (h *Handler) BroadcastToDark(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.darkClients {
		select {
		case ch <- data:
		default:
		}
	}
}

func isLightSidePlayer(playerID string) bool {
	return playerID == "light" || playerID == "free-peoples" || playerID == "player1"
}
