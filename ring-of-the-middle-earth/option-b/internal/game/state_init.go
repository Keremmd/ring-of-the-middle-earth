package game

import (
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/graph"
)

// InitState creates a fresh GameState from config
func InitState(cfg *config.GameConfig) *GameState {
	gs := &GameState{
		Turn:      1,
		Status:    "WAITING",
		Units:     make(map[string]*UnitSnapshot),
		Regions:   make(map[string]*RegionState),
		Paths:     make(map[string]*PathState),
		RingBearer: &RingBearerState{},
		Orders:    make(map[string]*Order),
	}

	for id, rc := range cfg.Regions {
		gs.Regions[id] = &RegionState{
			ID:           id,
			ControlledBy: Side(rc.StartControl),
			ThreatLevel:  rc.StartThreat,
			Fortified:    false,
			UnitsPresent: []string{},
		}
	}

	for id, pc := range cfg.Paths {
		gs.Paths[id] = &PathState{
			ID:     id,
			Status: PathOpen,
		}
		_ = pc
	}

	for id, uc := range cfg.Units {
		snap := &UnitSnapshot{
			ID:       id,
			Region:   uc.StartRegion,
			Strength: uc.Strength,
			Status:   StatusActive,
			Route:    []string{},
		}
		if uc.Class == "RingBearer" {
			gs.RingBearer.TrueRegion = uc.StartRegion
			snap.Region = "" // never expose
		} else {
			// add unit to region
			if r, ok := gs.Regions[uc.StartRegion]; ok {
				r.UnitsPresent = append(r.UnitsPresent, id)
			}
		}
		gs.Units[id] = snap
	}

	return gs
}

// BuildGraph builds the adjacency graph from config
func BuildGraph(cfg *config.GameConfig) *graph.Graph {
	g := graph.New()
	for _, p := range cfg.Paths {
		g.AddEdge(p.From, p.To, p.Cost)
	}
	return g
}
