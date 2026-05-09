package game

import (
	"fmt"

	"github.com/rotr/option-b/internal/config"
)

// ValidationError represents a game order validation error
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidateOrder validates a player order against the current game state
// Returns nil if valid, ValidationError if not
func ValidateOrder(order *Order, gs *GameState, cfg *config.GameConfig, playerSide Side) *ValidationError {
	// Rule 1: Wrong turn
	if order.Turn != gs.Turn {
		return &ValidationError{Code: "WRONG_TURN", Message: fmt.Sprintf("expected turn %d, got %d", gs.Turn, order.Turn)}
	}

	// Rule 2: Unit does not belong to submitting player's side
	uc, ok := cfg.Units[order.UnitID]
	if !ok {
		return &ValidationError{Code: "INVALID_TARGET", Message: "unknown unit id"}
	}
	if Side(uc.Side) != playerSide {
		return &ValidationError{Code: "NOT_YOUR_UNIT", Message: "unit belongs to other side"}
	}

	// Rule 8: Duplicate unit order
	if _, exists := gs.Orders[order.UnitID]; exists {
		return &ValidationError{Code: "DUPLICATE_UNIT_ORDER", Message: "unit already has an order this turn"}
	}

	u, ok2 := gs.Units[order.UnitID]
	if !ok2 {
		return &ValidationError{Code: "INVALID_TARGET", Message: "unit state not found"}
	}

	switch order.OrderType {
	case "ASSIGN_ROUTE", "REDIRECT_UNIT":
		pathIDs := order.PathIDs
		if order.OrderType == "REDIRECT_UNIT" {
			pathIDs = order.NewPathIDs
		}
		// Rule 3/4: Ring Bearer path blocked or invalid
		if uc.Class == "RingBearer" && len(pathIDs) > 0 {
			firstPath := pathIDs[0]
			if ps, ok3 := gs.Paths[firstPath]; ok3 {
				if ps.Status == PathBlocked {
					return &ValidationError{Code: "PATH_BLOCKED", Message: "next path in route is blocked"}
				}
			}
		}

	case "BLOCK_PATH", "SEARCH_PATH":
		pathID := order.PathID
		pc, ok3 := cfg.Paths[pathID]
		if !ok3 {
			return &ValidationError{Code: "INVALID_PATH", Message: "path not found"}
		}
		// Rule 5: Unit not adjacent
		if u.Region != pc.From && u.Region != pc.To {
			return &ValidationError{Code: "UNIT_NOT_ADJACENT", Message: "unit must be in endpoint region"}
		}

	case "ATTACK_REGION":
		targetRegion := order.TargetRegion
		// Rule 6: Target not adjacent or not enemy-controlled
		isAdjacent := false
		for _, e := range cfg.Paths {
			if (e.From == u.Region && e.To == targetRegion) || (e.To == u.Region && e.From == targetRegion) {
				isAdjacent = true
				break
			}
		}
		if !isAdjacent {
			return &ValidationError{Code: "INVALID_TARGET", Message: "target region not adjacent"}
		}
		if r, ok3 := gs.Regions[targetRegion]; ok3 {
			if r.ControlledBy == playerSide {
				return &ValidationError{Code: "INVALID_TARGET", Message: "cannot attack own region"}
			}
		}

	case "MAIA_ABILITY":
		// Rule 7: Cooldown check
		if u.Cooldown > 0 {
			return &ValidationError{Code: "ABILITY_ON_COOLDOWN", Message: fmt.Sprintf("cooldown: %d turns remaining", u.Cooldown)}
		}
		if !uc.Maia {
			return &ValidationError{Code: "INVALID_TARGET", Message: "unit is not a Maia"}
		}
		if gs.SarumanDisabled && uc.Side == "SHADOW" && uc.StartRegion == "isengard" {
			return &ValidationError{Code: "MAIA_DISABLED", Message: "Saruman is disabled (Isengard fallen)"}
		}

	case "DESTROY_RING":
		// Only valid at mount-doom with no dark side unit present
		if gs.RingBearer.TrueRegion != "mount-doom" {
			return &ValidationError{Code: "DESTROY_CONDITION_NOT_MET", Message: "Ring Bearer not at Mount Doom"}
		}
	}

	return nil
}
