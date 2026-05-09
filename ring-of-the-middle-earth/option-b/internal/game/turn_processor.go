package game

import (
	"fmt"

	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/graph"
)

// TurnEvent is emitted during turn processing
type TurnEvent struct {
	Type    string
	Payload interface{}
}

// ProcessTurn executes all 13 steps of turn processing per spec Section 6
// Returns a list of events emitted during processing
func ProcessTurn(gs *GameState, cfg *config.GameConfig, g *graph.Graph) []TurnEvent {
	var events []TurnEvent

	// Step 1: Orders already collected in gs.Orders

	// Step 2: Process AssignRoute and RedirectUnit
	for _, order := range gs.Orders {
		switch order.OrderType {
		case "ASSIGN_ROUTE":
			if u, ok := gs.Units[order.UnitID]; ok {
				if cfg.Units[order.UnitID].Class == "RingBearer" {
					gs.RingBearer.Route = order.PathIDs
					gs.RingBearer.RouteIdx = 0
				} else {
					u.Route = order.PathIDs
					u.RouteIdx = 0
				}
				events = append(events, TurnEvent{Type: "RouteAssigned", Payload: order.UnitID})
			}
		case "REDIRECT_UNIT":
			if u, ok := gs.Units[order.UnitID]; ok {
				if cfg.Units[order.UnitID].Class == "RingBearer" {
					gs.RingBearer.Route = order.NewPathIDs
					gs.RingBearer.RouteIdx = 0
				} else {
					u.Route = order.NewPathIDs
					u.RouteIdx = 0
				}
				events = append(events, TurnEvent{Type: "RouteRedirected", Payload: order.UnitID})
			}
		}
	}

	// Step 3: Process BlockPath and SearchPath
	for _, order := range gs.Orders {
		switch order.OrderType {
		case "BLOCK_PATH":
			pathState, ok := gs.Paths[order.PathID]
			if !ok {
				continue
			}
			pathCfg := cfg.Paths[order.PathID]
			unitRegion := gs.Units[order.UnitID].Region
			if unitRegion == pathCfg.From || unitRegion == pathCfg.To {
				if pathState.Status == PathOpen || pathState.Status == PathThreatened {
					pathState.Status = PathBlocked
					pathState.BlockedBy = order.UnitID
					events = append(events, TurnEvent{Type: "PathBlocked", Payload: order.PathID})
					// Emit RouteCompromised for units with this path in their route
					for uid, u := range gs.Units {
						for _, pid := range u.Route {
							if pid == order.PathID {
								events = append(events, TurnEvent{Type: "RouteCompromised", Payload: uid})
								break
							}
						}
					}
				}
			}
		case "SEARCH_PATH":
			pathState, ok := gs.Paths[order.PathID]
			if !ok {
				continue
			}
			if pathState.SurveillanceLevel < 3 {
				pathState.SurveillanceLevel++
				if pathState.Status == PathOpen {
					pathState.Status = PathThreatened
				}
				events = append(events, TurnEvent{Type: "PathSearched", Payload: order.PathID})
			}
		}
	}

	// Revert BLOCKED paths whose blocking unit has moved away
	for pathID, pathState := range gs.Paths {
		if pathState.Status == PathBlocked && pathState.BlockedBy != "" {
			blockerUnit, ok := gs.Units[pathState.BlockedBy]
			if !ok || blockerUnit.Status != StatusActive {
				pathState.Status = PathOpen
				pathState.BlockedBy = ""
				events = append(events, TurnEvent{Type: "PathUnblocked", Payload: pathID})
				continue
			}
			pathCfg := cfg.Paths[pathID]
			if blockerUnit.Region != pathCfg.From && blockerUnit.Region != pathCfg.To {
				pathState.Status = PathOpen
				pathState.BlockedBy = ""
				events = append(events, TurnEvent{Type: "PathUnblocked", Payload: pathID})
			}
		}
	}

	// Step 4: Process ReinforceRegion and DeployNazgul
	for _, order := range gs.Orders {
		switch order.OrderType {
		case "REINFORCE_REGION":
			if u, ok := gs.Units[order.UnitID]; ok && u.Status == StatusActive {
				removeUnitFromRegion(gs, order.UnitID, u.Region)
				u.Region = order.TargetRegion
				addUnitToRegion(gs, order.UnitID, order.TargetRegion)
				events = append(events, TurnEvent{Type: "UnitMoved", Payload: fmt.Sprintf("%s->%s", order.UnitID, order.TargetRegion)})
			}
		case "DEPLOY_NAZGUL":
			if u, ok := gs.Units[order.UnitID]; ok && u.Status == StatusActive {
				removeUnitFromRegion(gs, order.UnitID, u.Region)
				u.Region = order.TargetRegion
				addUnitToRegion(gs, order.UnitID, order.TargetRegion)
				events = append(events, TurnEvent{Type: "NazgulDeployed", Payload: fmt.Sprintf("%s->%s", order.UnitID, order.TargetRegion)})
			}
		}
	}

	// Step 5: Process FortifyRegion
	for _, order := range gs.Orders {
		if order.OrderType == "FORTIFY_REGION" {
			uc := cfg.Units[order.UnitID]
			if uc.CanFortify {
				u := gs.Units[order.UnitID]
				if r, ok := gs.Regions[u.Region]; ok {
					r.Fortified = true
					r.FortifyTurns = 2
					events = append(events, TurnEvent{Type: "RegionFortified", Payload: u.Region})
				}
			}
		}
	}

	// Step 6: Process MaiaAbility orders
	for _, order := range gs.Orders {
		if order.OrderType == "MAIA_ABILITY" {
			uc, ok := cfg.Units[order.UnitID]
			if !ok || !uc.Maia {
				continue
			}
			u := gs.Units[order.UnitID]
			if u.Cooldown > 0 {
				continue
			}
			pathState, ok2 := gs.Paths[order.TargetPathID]
			if !ok2 {
				continue
			}

			// Dispatch based on config, not unit ID
			switch uc.Class {
			case "Maia":
				if uc.Side == "FREE_PEOPLES" {
					// Gandalf: OpenPath
					if pathState.Status == PathBlocked {
						pathState.Status = PathTemporarilyOpen
						pathState.TempOpenTurns = 2
						u.Cooldown = uc.Cooldown
						events = append(events, TurnEvent{Type: "PathOpened", Payload: order.TargetPathID})
					}
				} else if uc.Side == "SHADOW" && !gs.SarumanDisabled {
					// Saruman: CorruptPath — check maiaAbilityPaths
					allowed := false
					for _, ap := range uc.MaiaAbilityPaths {
						if ap == order.TargetPathID {
							allowed = true
							break
						}
					}
					if allowed {
						pathState.SurveillanceLevel = 3
						pathState.Corrupted = true
						u.Cooldown = uc.Cooldown
						events = append(events, TurnEvent{Type: "PathCorrupted", Payload: order.TargetPathID})
					}
				}
				// Sauron: passive, no order needed
			}
		}
	}

	// Step 7: Auto-advance all units with assigned routes
	advanceUnits(gs, cfg, g, &events)

	// Step 8: Process AttackRegion
	for _, order := range gs.Orders {
		if order.OrderType == "ATTACK_REGION" {
			resolveAttackOrder(gs, cfg, order, &events)
		}
	}

	// Step 9: Decrement TEMPORARILY_OPEN timers
	for pathID, pathState := range gs.Paths {
		if pathState.Status == PathTemporarilyOpen {
			pathState.TempOpenTurns--
			if pathState.TempOpenTurns <= 0 {
				if pathState.BlockedBy != "" {
					if bu, ok := gs.Units[pathState.BlockedBy]; ok && bu.Status == StatusActive {
						pc := cfg.Paths[pathID]
						if bu.Region == pc.From || bu.Region == pc.To {
							pathState.Status = PathBlocked
						} else {
							pathState.Status = PathOpen
							pathState.BlockedBy = ""
						}
					} else {
						pathState.Status = PathOpen
						pathState.BlockedBy = ""
					}
				} else {
					pathState.Status = PathOpen
				}
			}
		}
	}

	// Step 10: Decrement fortification timers
	for _, region := range gs.Regions {
		if region.Fortified {
			region.FortifyTurns--
			if region.FortifyTurns <= 0 {
				region.Fortified = false
				events = append(events, TurnEvent{Type: "FortificationExpired", Payload: region.ID})
			}
		}
	}

	// Step 11: Decrement respawn and cooldown counters
	for id, u := range gs.Units {
		if u.Status == StatusRespawning {
			u.RespawnTurns--
			if u.RespawnTurns <= 0 {
				uc := cfg.Units[id]
				u.Status = StatusActive
				u.Strength = uc.Strength
				u.Region = uc.StartRegion
				addUnitToRegion(gs, id, uc.StartRegion)
				events = append(events, TurnEvent{Type: "UnitRespawned", Payload: id})
			}
		}
		if u.Cooldown > 0 {
			u.Cooldown--
		}
	}

	// Step 12: Run detection check
	detection := RunDetection(gs, cfg, g, gs.Turn, cfg.HiddenUntilTurn)
	gs.RingBearer.Exposed = detection.Exposed
	if detection.Exposed {
		events = append(events, TurnEvent{Type: "RingBearerDetected", Payload: detection.TrueRegion})
	}

	// Step 13: Evaluate win conditions
	winner := evaluateWinConditions(gs, cfg)
	if winner != "" || gs.Turn >= cfg.MaxTurns {
		cause := "RING_DESTROYED"
		if gs.Turn >= cfg.MaxTurns && winner == "" {
			winner = "DRAW"
			cause = "MAX_TURNS"
		} else if winner == "SHADOW" {
			cause = "RING_BEARER_CAUGHT"
		}
		events = append(events, TurnEvent{Type: "GameOver", Payload: GameOverEvent{Winner: winner, Cause: cause, Turn: gs.Turn}})
		gs.Status = "FINISHED"
		gs.Winner = winner
	}

	// Reset exposed
	gs.RingBearer.Exposed = false

	// Clear orders for next turn
	gs.Orders = make(map[string]*Order)
	gs.Turn++

	return events
}

func advanceUnits(gs *GameState, cfg *config.GameConfig, g *graph.Graph, events *[]TurnEvent) {
	// Ring Bearer auto-advance
	rb := gs.RingBearer
	if len(rb.Route) > 0 && rb.RouteIdx < len(rb.Route) {
		pathID := rb.Route[rb.RouteIdx]
		pathState, ok := gs.Paths[pathID]
		if ok {
			canMove := pathState.Status == PathOpen || pathState.Status == PathThreatened || pathState.Status == PathTemporarilyOpen
			if canMove {
				pathCfg := cfg.Paths[pathID]
				nextRegion := pathCfg.To
				if pathCfg.From == rb.TrueRegion {
					nextRegion = pathCfg.To
				} else {
					nextRegion = pathCfg.From
				}
				rb.TrueRegion = nextRegion
				rb.RouteIdx++
				// Check surveillance exposure
				if pathState.SurveillanceLevel >= 1 && gs.Turn > cfg.HiddenUntilTurn {
					rb.Exposed = true
					*events = append(*events, TurnEvent{Type: "RingBearerSpotted", Payload: pathID})
				}
				*events = append(*events, TurnEvent{Type: "RingBearerMoved", Payload: nextRegion})
				if rb.RouteIdx >= len(rb.Route) {
					*events = append(*events, TurnEvent{Type: "RouteComplete", Payload: "ring-bearer"})
					// Auto-trigger DESTROY_RING when Ring Bearer completes route at Mount Doom
					if nextRegion == "mount-doom" {
						gs.Orders["__destroy_ring__"] = &Order{OrderType: "DESTROY_RING", UnitID: "ring-bearer"}
						*events = append(*events, TurnEvent{Type: "DestroyRingAttempt", Payload: "mount-doom"})
					}
				}
			} else {
				*events = append(*events, TurnEvent{Type: "RouteBlocked", Payload: "ring-bearer"})
			}
		}
	}

	// Other units auto-advance
	for id, u := range gs.Units {
		uc := cfg.Units[id]
		if uc.Class == "RingBearer" || u.Status != StatusActive {
			continue
		}
		if len(u.Route) == 0 || u.RouteIdx >= len(u.Route) {
			continue
		}
		pathID := u.Route[u.RouteIdx]
		pathState, ok := gs.Paths[pathID]
		if !ok {
			continue
		}
		canMove := pathState.Status == PathOpen || pathState.Status == PathThreatened || pathState.Status == PathTemporarilyOpen
		if canMove {
			pathCfg := cfg.Paths[pathID]
			nextRegion := pathCfg.To
			if pathCfg.From == u.Region {
				nextRegion = pathCfg.To
			} else {
				nextRegion = pathCfg.From
			}
			removeUnitFromRegion(gs, id, u.Region)
			u.Region = nextRegion
			addUnitToRegion(gs, id, nextRegion)
			u.RouteIdx++
			*events = append(*events, TurnEvent{Type: "UnitMoved", Payload: fmt.Sprintf("%s->%s", id, nextRegion)})
			if u.RouteIdx >= len(u.Route) {
				*events = append(*events, TurnEvent{Type: "RouteComplete", Payload: id})
			}
		} else {
			*events = append(*events, TurnEvent{Type: "RouteBlocked", Payload: id})
		}
	}

	_ = g
}

func resolveAttackOrder(gs *GameState, cfg *config.GameConfig, order *Order, events *[]TurnEvent) {
	targetRegion, ok := gs.Regions[order.TargetRegion]
	if !ok {
		return
	}

	attackerUnit := gs.Units[order.UnitID]
	if attackerUnit == nil || attackerUnit.Status != StatusActive {
		return
	}

	// Gather all attackers in same region as the attacker
	var attackerIDs []string
	for uid, u := range gs.Units {
		if u.Status == StatusActive && u.Region == attackerUnit.Region {
			uc := cfg.Units[uid]
			if string(uc.Side) == string(cfg.Units[order.UnitID].Side) {
				attackerIDs = append(attackerIDs, uid)
			}
		}
	}

	// Defenders: all units of the other side in the target region
	attackerSide := cfg.Units[order.UnitID].Side
	var defenderIDs []string
	for uid, u := range gs.Units {
		if u.Status == StatusActive && u.Region == order.TargetRegion {
			uc := cfg.Units[uid]
			if uc.Side != attackerSide {
				defenderIDs = append(defenderIDs, uid)
			}
		}
	}

	result := ResolveAttack(attackerIDs, defenderIDs, cfg.Units, gs.Units, cfg.Regions[order.TargetRegion].Terrain, targetRegion.Fortified)

	if result.AttackerWon {
		// Apply damage to defenders
		for _, did := range defenderIDs {
			du := gs.Units[did]
			dc := cfg.Units[did]
			ApplyDamage(du, dc, result.Damage)
			if du.Status != StatusActive {
				removeUnitFromRegion(gs, did, order.TargetRegion)
			}
		}
		targetRegion.ControlledBy = Side(attackerSide)
		// Move attackers into target region
		for _, aid := range attackerIDs {
			removeUnitFromRegion(gs, aid, gs.Units[aid].Region)
			gs.Units[aid].Region = order.TargetRegion
			addUnitToRegion(gs, aid, order.TargetRegion)
		}
		*events = append(*events, TurnEvent{Type: "BattleResolved", Payload: fmt.Sprintf("ATTACKER_WON:%s", order.TargetRegion)})

		// Check if Isengard fell
		if order.TargetRegion == "isengard" && attackerSide == "FREE_PEOPLES" {
			gs.SarumanDisabled = true
			// Disable Saruman
			for id, uc := range cfg.Units {
				if uc.Maia && uc.Side == "SHADOW" && uc.StartRegion == "isengard" {
					if u, ok2 := gs.Units[id]; ok2 {
						u.Status = StatusDestroyed
						removeUnitFromRegion(gs, id, u.Region)
					}
					break
				}
			}
			*events = append(*events, TurnEvent{Type: "IsengardDestroyed", Payload: "isengard"})
		}
	} else {
		// Attackers each lose 1 strength
		for _, aid := range attackerIDs {
			ac := cfg.Units[aid]
			ApplyDamage(gs.Units[aid], ac, result.Damage)
		}
		*events = append(*events, TurnEvent{Type: "BattleResolved", Payload: fmt.Sprintf("DEFENDER_WON:%s", order.TargetRegion)})
	}
}

func evaluateWinConditions(gs *GameState, cfg *config.GameConfig) string {
	// Light Side wins when:
	// Ring Bearer in mount-doom AND DestroyRing order was submitted AND no Dark Side unit in mount-doom
	_, destroySubmitted := gs.Orders["__destroy_ring__"]
	if gs.RingBearer.TrueRegion == "mount-doom" && destroySubmitted {
		darkUnitAtMountDoom := false
		for id, u := range gs.Units {
			if u.Status == StatusActive && u.Region == "mount-doom" {
				if cfg.Units[id].Side == "SHADOW" {
					darkUnitAtMountDoom = true
					break
				}
			}
		}
		if !darkUnitAtMountDoom {
			return "FREE_PEOPLES"
		}
	}

	// Dark Side wins when:
	// Any Nazgul occupies same region as Ring Bearer AND exposed==true
	if gs.RingBearer.Exposed {
		for id, u := range gs.Units {
			if cfg.Units[id].Class == "Nazgul" && u.Status == StatusActive && u.Region == gs.RingBearer.TrueRegion {
				return "SHADOW"
			}
		}
	}

	return ""
}

func removeUnitFromRegion(gs *GameState, unitID, regionID string) {
	if r, ok := gs.Regions[regionID]; ok {
		newPresent := r.UnitsPresent[:0]
		for _, id := range r.UnitsPresent {
			if id != unitID {
				newPresent = append(newPresent, id)
			}
		}
		r.UnitsPresent = newPresent
	}
}

func addUnitToRegion(gs *GameState, unitID, regionID string) {
	if r, ok := gs.Regions[regionID]; ok {
		for _, id := range r.UnitsPresent {
			if id == unitID {
				return
			}
		}
		r.UnitsPresent = append(r.UnitsPresent, unitID)
	}
}
