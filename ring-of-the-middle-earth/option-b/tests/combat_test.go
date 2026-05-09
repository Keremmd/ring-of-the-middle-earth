package tests

import (
	"testing"

	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/game"
)

// combat_test.go — 6 required cases per Section 35 (Option B)

func makeCombatUnit(id string, strength int, leadership bool, leadershipBonus int, ignoresFortress bool, indestructible bool, respawns bool, respawnTurns int) (config.UnitConfig, *game.UnitSnapshot) {
	cfg := config.UnitConfig{
		ID:              id,
		Strength:        strength,
		Leadership:      leadership,
		LeadershipBonus: leadershipBonus,
		IgnoresFortress: ignoresFortress,
		Indestructible:  indestructible,
		Respawns:        respawns,
		RespawnTurns:    respawnTurns,
	}
	snap := &game.UnitSnapshot{
		ID:       id,
		Strength: strength,
		Status:   game.StatusActive,
	}
	return cfg, snap
}

// Case 1: Attacker(5) vs Defender(5, PLAINS) → tie, attacker repelled
func TestCombat_TiePlains(t *testing.T) {
	attackerCfg, attackerSnap := makeCombatUnit("a1", 5, false, 0, false, false, false, 0)
	defenderCfg, defenderSnap := makeCombatUnit("d1", 5, false, 0, false, false, false, 0)

	unitCfgs := map[string]config.UnitConfig{
		attackerCfg.ID: attackerCfg,
		defenderCfg.ID: defenderCfg,
	}
	units := map[string]*game.UnitSnapshot{
		attackerSnap.ID: attackerSnap,
		defenderSnap.ID: defenderSnap,
	}

	result := game.ResolveAttack(
		[]string{attackerCfg.ID},
		[]string{defenderCfg.ID},
		unitCfgs,
		units,
		"PLAINS",
		false,
	)

	if result.AttackerWon {
		t.Errorf("Case 1: expected attacker repelled (tie), got attacker won")
	}
}

// Case 2: Attacker(5) vs Defender(5, FORTRESS) → defender wins (5 vs 7)
func TestCombat_FortressTerrain(t *testing.T) {
	attackerCfg, attackerSnap := makeCombatUnit("a2", 5, false, 0, false, false, false, 0)
	defenderCfg, defenderSnap := makeCombatUnit("d2", 5, false, 0, false, false, false, 0)

	unitCfgs := map[string]config.UnitConfig{
		attackerCfg.ID: attackerCfg,
		defenderCfg.ID: defenderCfg,
	}
	units := map[string]*game.UnitSnapshot{
		attackerSnap.ID: attackerSnap,
		defenderSnap.ID: defenderSnap,
	}

	result := game.ResolveAttack(
		[]string{attackerCfg.ID},
		[]string{defenderCfg.ID},
		unitCfgs,
		units,
		"FORTRESS",
		false,
	)

	if result.AttackerWon {
		t.Errorf("Case 2: expected defender wins in FORTRESS (5 vs 7), got attacker won")
	}
}

// Case 3: UrukHai(5, ignoresFortress) vs Defender(5, FORTRESS) → tie (5 vs 5)
func TestCombat_UrukHaiIgnoresFortress(t *testing.T) {
	urukCfg, urukSnap := makeCombatUnit("uruk", 5, false, 0, true, false, false, 0)
	defenderCfg, defenderSnap := makeCombatUnit("d3", 5, false, 0, false, false, false, 0)

	unitCfgs := map[string]config.UnitConfig{
		urukCfg.ID:     urukCfg,
		defenderCfg.ID: defenderCfg,
	}
	units := map[string]*game.UnitSnapshot{
		urukSnap.ID:    urukSnap,
		defenderSnap.ID: defenderSnap,
	}

	result := game.ResolveAttack(
		[]string{urukCfg.ID},
		[]string{defenderCfg.ID},
		unitCfgs,
		units,
		"FORTRESS",
		false,
	)

	// ignoresFortress: terrain bonus skipped → 5 vs 5 → tie
	if result.AttackerWon {
		t.Errorf("Case 3: UrukHai(ignoresFortress) vs 5 FORTRESS should be tie, got attacker won")
	}
}

// Case 4: UrukHai(5) vs Defender(5, FORTRESS, fortified) → defender wins (5 vs 7)
// fortification_bonus applies even vs ignoresFortress
func TestCombat_UrukHaiFortified(t *testing.T) {
	urukCfg, urukSnap := makeCombatUnit("uruk2", 5, false, 0, true, false, false, 0)
	defenderCfg, defenderSnap := makeCombatUnit("d4", 5, false, 0, false, false, false, 0)

	unitCfgs := map[string]config.UnitConfig{
		urukCfg.ID:     urukCfg,
		defenderCfg.ID: defenderCfg,
	}
	units := map[string]*game.UnitSnapshot{
		urukSnap.ID:    urukSnap,
		defenderSnap.ID: defenderSnap,
	}

	result := game.ResolveAttack(
		[]string{urukCfg.ID},
		[]string{defenderCfg.ID},
		unitCfgs,
		units,
		"FORTRESS",
		true, // fortified
	)

	// ignoresFortress skips terrain bonus (+2) but NOT fortification bonus (+2)
	// attacker_power = 5, defender_power = 5 + 0 (terrain skipped) + 2 (fortify) = 7
	// 5 < 7 → defender wins
	if result.AttackerWon {
		t.Errorf("Case 4: UrukHai(ignoresFortress) vs fortified FORTRESS: expected defender wins (5 vs 7)")
	}
}

// Case 5: Leadership bonus applied correctly to co-located allies
// Aragorn(5, leader+1) + Gimli(3) attack → Gimli effective=4; 5+4=9 vs 5 PLAINS → attacker wins
func TestCombat_LeadershipBonus(t *testing.T) {
	aragornCfg, aragornSnap := makeCombatUnit("aragorn", 5, true, 1, false, false, false, 0)
	gimliCfg, gimliSnap := makeCombatUnit("gimli", 3, false, 0, false, false, false, 0)
	defenderCfg, defenderSnap := makeCombatUnit("defender", 5, false, 0, false, false, false, 0)

	unitCfgs := map[string]config.UnitConfig{
		aragornCfg.ID:  aragornCfg,
		gimliCfg.ID:    gimliCfg,
		defenderCfg.ID: defenderCfg,
	}
	units := map[string]*game.UnitSnapshot{
		aragornSnap.ID:  aragornSnap,
		gimliSnap.ID:    gimliSnap,
		defenderSnap.ID: defenderSnap,
	}

	result := game.ResolveAttack(
		[]string{aragornCfg.ID, gimliCfg.ID},
		[]string{defenderCfg.ID},
		unitCfgs,
		units,
		"PLAINS",
		false,
	)

	// attacker_power: Aragorn=5 (leader) + Gimli=3+1=4 = 9
	// defender_power: 5
	// 9 > 5 → attacker wins
	if !result.AttackerWon {
		t.Errorf("Case 5: Aragorn(5,+1) + Gimli(3) should beat defender(5 PLAINS): expected attacker wins")
	}
}

// Case 6: Indestructible unit — strength floors at 1
func TestCombat_IndestructibleFloor(t *testing.T) {
	witchKingCfg := config.UnitConfig{
		ID:             "witch-king",
		Strength:       5,
		Indestructible: true,
	}
	witchKing := &game.UnitSnapshot{
		ID:       "witch-king",
		Strength: 5,
		Status:   game.StatusActive,
	}

	// Apply lethal damage
	game.ApplyDamage(witchKing, witchKingCfg, 10)

	if witchKing.Strength != 1 {
		t.Errorf("Case 6: indestructible unit should floor at 1, got strength=%d", witchKing.Strength)
	}
	if witchKing.Status != game.StatusActive {
		t.Errorf("Case 6: indestructible unit should remain ACTIVE, got status=%s", witchKing.Status)
	}
}

// Additional: Non-respawning unit takes fatal damage → DESTROYED
func TestCombat_FatalDamageDestroyed(t *testing.T) {
	cfg := config.UnitConfig{
		ID:       "legolas",
		Strength: 3,
		Respawns: false,
	}
	unit := &game.UnitSnapshot{
		ID:       "legolas",
		Strength: 3,
		Status:   game.StatusActive,
	}

	game.ApplyDamage(unit, cfg, 5)

	if unit.Status != game.StatusDestroyed {
		t.Errorf("Non-respawning unit with fatal damage should be DESTROYED, got %s", unit.Status)
	}
}

// Additional: Respawning unit takes fatal damage → RESPAWNING
func TestCombat_FatalDamageRespawning(t *testing.T) {
	cfg := config.UnitConfig{
		ID:           "nazgul-2",
		Strength:     3,
		Respawns:     true,
		RespawnTurns: 3,
	}
	unit := &game.UnitSnapshot{
		ID:       "nazgul-2",
		Strength: 3,
		Status:   game.StatusActive,
	}

	game.ApplyDamage(unit, cfg, 5)

	if unit.Status != game.StatusRespawning {
		t.Errorf("Respawning unit with fatal damage should be RESPAWNING, got %s", unit.Status)
	}
	if unit.RespawnTurns != 3 {
		t.Errorf("Respawning unit should have respawnTurns=3, got %d", unit.RespawnTurns)
	}
}
