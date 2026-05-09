package game

import (
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/graph"
)

// DetectionResult holds what was detected this turn
type DetectionResult struct {
	Exposed    bool
	TrueRegion string
	ByPath     string // path that caused exposure via surveillanceLevel
}

// RunDetection runs the detection formula for all Nazgul against the Ring Bearer.
// Returns whether the Ring Bearer is exposed and the true region.
// suppressed = turn <= hiddenUntilTurn
func RunDetection(
	gs *GameState,
	cfg *config.GameConfig,
	g *graph.Graph,
	turn int,
	hiddenUntilTurn int,
) DetectionResult {
	if turn <= hiddenUntilTurn {
		return DetectionResult{Exposed: false}
	}

	rb := gs.RingBearer
	exposed := false

	// Check if Sauron is active in Mordor (passive Eye of Sauron)
	sauronBonus := 0
	for id, uc := range cfg.Units {
		if uc.Maia && uc.Side == "SHADOW" && uc.StartRegion == "mordor" {
			// Sauron - check if active in mordor
			if u, ok := gs.Units[id]; ok && u.Status == StatusActive && u.Region == "mordor" {
				_ = u
				sauronBonus = 1
			}
			break
		}
	}

	// Run detection for each Nazgul
	for id, uc := range cfg.Units {
		if uc.Class != "Nazgul" {
			continue
		}
		u, ok := gs.Units[id]
		if !ok || u.Status != StatusActive {
			continue
		}

		effectiveRange := uc.DetectionRange + sauronBonus
		dist := g.Distance(u.Region, rb.TrueRegion)
		if dist <= effectiveRange {
			exposed = true
			break
		}
	}

	return DetectionResult{
		Exposed:    exposed,
		TrueRegion: rb.TrueRegion,
	}
}
