package cache

import (
	"sync"

	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/game"
)

// WorldStateCache holds the current view of the game world
// It is owned by the CacheManager goroutine; callers receive value copies
type WorldStateCache struct {
	mu sync.RWMutex

	Turn        int
	Status      string
	Winner      string
	Units       map[string]game.UnitSnapshot
	Regions     map[string]game.RegionState
	Paths       map[string]game.PathState
	UnitConfigs map[string]config.UnitConfig // read-only after startup
	LightView   LightSideView
	DarkView    DarkSideView
}

// LightSideView contains Light Side-only data
type LightSideView struct {
	RingBearerRegion string
	AssignedRoute    []string
	RouteIdx         int
}

// DarkSideView contains Dark Side-only data
// RingBearerRegion is ALWAYS "" — enforced by EventRouter
type DarkSideView struct {
	RingBearerRegion   string // ALWAYS ""
	LastDetectedRegion string
	LastDetectedTurn   int
}

// NewWorldStateCache creates and returns an empty cache
func NewWorldStateCache(unitCfgs map[string]config.UnitConfig) *WorldStateCache {
	return &WorldStateCache{
		Units:       make(map[string]game.UnitSnapshot),
		Regions:     make(map[string]game.RegionState),
		Paths:       make(map[string]game.PathState),
		UnitConfigs: unitCfgs,
		DarkView:    DarkSideView{RingBearerRegion: ""}, // enforced: always ""
	}
}

// Update applies a full world state snapshot to the cache
func (c *WorldStateCache) Update(snap game.WorldStateSnapshot, rbRegion string, rbRoute []string, rbIdx int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Turn = snap.Turn
	c.Status = snap.Status
	c.Winner = snap.Winner
	for _, r := range snap.Regions {
		if r != nil {
			c.Regions[r.ID] = *r
		}
	}
	for _, u := range snap.Units {
		if u != nil {
			c.Units[u.ID] = *u
		}
	}
	// Update light view
	c.LightView = LightSideView{
		RingBearerRegion: rbRegion,
		AssignedRoute:    rbRoute,
		RouteIdx:         rbIdx,
	}
	// Dark view: RingBearerRegion stays ""
	c.DarkView.RingBearerRegion = "" // ENFORCED
}

// UpdateDetection updates the Dark Side detection data
func (c *WorldStateCache) UpdateDetection(region string, turn int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DarkView.LastDetectedRegion = region
	c.DarkView.LastDetectedTurn = turn
	c.DarkView.RingBearerRegion = "" // ENFORCED
}

// Reset clears the cache to a clean initial state (called on game restart)
func (c *WorldStateCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Turn = 0
	c.Status = ""
	c.Winner = ""
	c.Units = make(map[string]game.UnitSnapshot)
	c.Regions = make(map[string]game.RegionState)
	c.Paths = make(map[string]game.PathState)
	c.LightView = LightSideView{}
	c.DarkView = DarkSideView{RingBearerRegion: ""}
}

// UpdatePath updates a single path state
func (c *WorldStateCache) UpdatePath(ps game.PathState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Paths[ps.ID] = ps
}

// Snapshot returns a deep copy of the cache for use by workers
func (c *WorldStateCache) Snapshot() WorldStateCache {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snap := WorldStateCache{
		Turn:        c.Turn,
		Status:      c.Status,
		Winner:      c.Winner,
		Units:       make(map[string]game.UnitSnapshot, len(c.Units)),
		Regions:     make(map[string]game.RegionState, len(c.Regions)),
		Paths:       make(map[string]game.PathState, len(c.Paths)),
		UnitConfigs: c.UnitConfigs, // read-only, safe to share
		LightView:   c.LightView,
		DarkView:    DarkSideView{RingBearerRegion: ""}, // ENFORCED
	}
	snap.DarkView.LastDetectedRegion = c.DarkView.LastDetectedRegion
	snap.DarkView.LastDetectedTurn = c.DarkView.LastDetectedTurn

	for k, v := range c.Units {
		snap.Units[k] = v
	}
	for k, v := range c.Regions {
		snap.Regions[k] = v
	}
	for k, v := range c.Paths {
		snap.Paths[k] = v
	}
	return snap
}
