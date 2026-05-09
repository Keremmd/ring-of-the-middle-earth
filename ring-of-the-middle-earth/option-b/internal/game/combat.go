package game

import (
	"github.com/rotr/option-b/internal/config"
)

// CombatResult holds the outcome of a battle
type CombatResult struct {
	AttackerWon bool
	Damage      int
}

// terrainBonus returns the defensive terrain bonus for a region
func terrainBonus(terrain string) int {
	switch terrain {
	case "FORTRESS":
		return 2
	case "MOUNTAINS":
		return 1
	default:
		return 0
	}
}

// ResolveAttack resolves combat between attackers and defenders in a region.
// attackerIDs and defenderIDs are unit IDs present in the battle.
// unitCfgs provides unit configuration (read-only, config-driven).
// units provides current unit snapshots.
// regionTerrain is the terrain of the defending region.
// fortified indicates if the region is fortified by GondorArmy.
func ResolveAttack(
	attackerIDs []string,
	defenderIDs []string,
	unitCfgs map[string]config.UnitConfig,
	units map[string]*UnitSnapshot,
	regionTerrain string,
	fortified bool,
) CombatResult {
	// Find if any attacker ignores fortress
	ignoresFortress := false
	for _, aid := range attackerIDs {
		if cfg, ok := unitCfgs[aid]; ok && cfg.IgnoresFortress {
			ignoresFortress = true
			break
		}
	}

	// Compute effective strengths with leadership bonus
	attackerPower := computeSidePower(attackerIDs, attackerIDs, unitCfgs, units)
	defenderPower := computeSidePower(defenderIDs, defenderIDs, unitCfgs, units)

	// Add terrain bonus to defender (skip if ignoresFortress)
	if !ignoresFortress {
		defenderPower += terrainBonus(regionTerrain)
	}

	// Add fortification bonus (always applies, even vs ignoresFortress)
	if fortified {
		defenderPower += 2
	}

	if attackerPower > defenderPower {
		damage := attackerPower - defenderPower
		return CombatResult{AttackerWon: true, Damage: damage}
	}

	return CombatResult{AttackerWon: false, Damage: 1}
}

// computeSidePower computes the total effective power for a group of units,
// applying leadership bonuses from leaders within the same group
func computeSidePower(unitIDs []string, allAllies []string, unitCfgs map[string]config.UnitConfig, units map[string]*UnitSnapshot) int {
	// Collect leaders in the ally group
	leaderBonus := 0
	for _, id := range allAllies {
		if cfg, ok := unitCfgs[id]; ok && cfg.Leadership {
			if u, ok2 := units[id]; ok2 && u.Status == StatusActive {
				leaderBonus += cfg.LeadershipBonus
			}
		}
	}

	total := 0
	for _, id := range unitIDs {
		u, ok := units[id]
		if !ok || u.Status != StatusActive {
			continue
		}
		cfg, ok2 := unitCfgs[id]
		if !ok2 {
			continue
		}
		effective := u.Strength
		// Non-leader units get leadership bonus from co-located leaders
		if !cfg.Leadership {
			effective += leaderBonus
		}
		total += effective
	}
	return total
}

// ApplyDamage applies damage to a unit, respecting indestructible and respawns config
func ApplyDamage(unit *UnitSnapshot, cfg config.UnitConfig, damage int) {
	raw := unit.Strength - damage
	if cfg.Indestructible {
		if raw < 1 {
			unit.Strength = 1
		} else {
			unit.Strength = raw
		}
		unit.Status = StatusActive
		return
	}
	if raw <= 0 {
		unit.Strength = 0
		if cfg.Respawns {
			unit.Status = StatusRespawning
			unit.RespawnTurns = cfg.RespawnTurns
			unit.Region = ""
		} else {
			unit.Status = StatusDestroyed
		}
		return
	}
	unit.Strength = raw
	unit.Status = StatusActive
}
