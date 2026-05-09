package game

// UnitStatus represents the lifecycle state of a unit
type UnitStatus string

const (
	StatusActive     UnitStatus = "ACTIVE"
	StatusDestroyed  UnitStatus = "DESTROYED"
	StatusRespawning UnitStatus = "RESPAWNING"
)

// PathStatus represents the current traversability state of a path
type PathStatus string

const (
	PathOpen            PathStatus = "OPEN"
	PathThreatened      PathStatus = "THREATENED"
	PathBlocked         PathStatus = "BLOCKED"
	PathTemporarilyOpen PathStatus = "TEMPORARILY_OPEN"
)

// Side represents which faction a unit or region belongs to
type Side string

const (
	SideFreePeoples Side = "FREE_PEOPLES"
	SideShadow      Side = "SHADOW"
	SideNeutral     Side = "NEUTRAL"
)

// UnitSnapshot is the runtime state of a unit
type UnitSnapshot struct {
	ID           string     `json:"id"`
	Region       string     `json:"region"` // always "" for ring-bearer in public state
	Strength     int        `json:"strength"`
	Status       UnitStatus `json:"status"`
	RespawnTurns int        `json:"respawnTurns"`
	Route        []string   `json:"route"`
	RouteIdx     int        `json:"routeIdx"`
	Cooldown     int        `json:"cooldown"`
}

// RegionState is the runtime state of a region
type RegionState struct {
	ID           string   `json:"id"`
	ControlledBy Side     `json:"controlledBy"`
	ThreatLevel  int      `json:"threatLevel"`
	Fortified    bool     `json:"fortified"`
	FortifyTurns int      `json:"fortifyTurns"`
	UnitsPresent []string `json:"unitsPresent"`
}

// PathState is the runtime state of a path
type PathState struct {
	ID                string     `json:"id"`
	Status            PathStatus `json:"status"`
	SurveillanceLevel int        `json:"surveillanceLevel"`
	TempOpenTurns     int        `json:"tempOpenTurns"`
	BlockedBy         string     `json:"blockedBy"`
	Corrupted         bool       `json:"corrupted"`
}

// RingBearerState holds the secret Ring Bearer data — never exposed publicly
type RingBearerState struct {
	TrueRegion         string
	Exposed            bool
	Route              []string
	RouteIdx           int
	LastDetectedTurn   int
	LastDetectedRegion string
}

// Order represents a player-submitted game order
type Order struct {
	OrderType    string   `json:"orderType"`
	PlayerID     string   `json:"playerId"`
	UnitID       string   `json:"unitId"`
	Turn         int      `json:"turn"`
	PathIDs      []string `json:"pathIds,omitempty"`
	NewPathIDs   []string `json:"newPathIds,omitempty"`
	TargetRegion string   `json:"targetRegion,omitempty"`
	TargetPathID string   `json:"targetPathId,omitempty"`
	PathID       string   `json:"pathId,omitempty"`
}

// GameState is the full runtime state of the game
type GameState struct {
	Turn            int
	SessionID       string
	Status          string // "WAITING" | "ACTIVE" | "FINISHED"
	Winner          string // "" | "FREE_PEOPLES" | "SHADOW" | "DRAW"
	Units           map[string]*UnitSnapshot
	Regions         map[string]*RegionState
	Paths           map[string]*PathState
	RingBearer      *RingBearerState
	SarumanDisabled bool
	Orders          map[string]*Order // keyed by unitId, reset each turn
}

// WorldStateSnapshot is emitted at the end of each turn (game.broadcast)
type WorldStateSnapshot struct {
	Turn      int             `json:"turn"`
	Regions   []*RegionState  `json:"regions"`
	Units     []*UnitSnapshot `json:"units"`
	Status    string          `json:"status,omitempty"`
	Winner    string          `json:"winner,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

// GameEvent types
type GameOverEvent struct {
	Winner string `json:"winner"`
	Cause  string `json:"cause"`
	Turn   int    `json:"turn"`
}
