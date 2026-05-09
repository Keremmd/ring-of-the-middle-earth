package config

import (
	"encoding/json"
	"os"
)

type UnitConfig struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Class            string   `json:"class"`
	Side             string   `json:"side"`
	StartRegion      string   `json:"start"`
	Strength         int      `json:"strength"`
	Leadership       bool     `json:"leadership"`
	LeadershipBonus  int      `json:"leadershipBonus"`
	Indestructible   bool     `json:"indestructible"`
	DetectionRange   int      `json:"detectionRange"`
	Respawns         bool     `json:"respawns"`
	RespawnTurns     int      `json:"respawnTurns"`
	Maia             bool     `json:"maia"`
	MaiaAbilityPaths []string `json:"maiaAbilityPaths"`
	IgnoresFortress  bool     `json:"ignoresFortress"`
	CanFortify       bool     `json:"canFortify"`
	Cooldown         int      `json:"cooldown"`
}

type RegionConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Terrain      string `json:"terrain"`
	SpecialRole  string `json:"specialRole"`
	StartControl string `json:"startControl"`
	StartThreat  int    `json:"startThreat"`
}

type PathConfig struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
	Cost int    `json:"cost"`
}

type UnitsFile struct {
	HiddenUntilTurn int          `json:"hidden-until-turn"`
	MaxTurns        int          `json:"max-turns"`
	TurnDurationSec int          `json:"turn-duration-seconds"`
	Units           []UnitConfig `json:"units"`
}

type MapFile struct {
	Regions []RegionConfig `json:"regions"`
	Paths   []PathConfig   `json:"paths"`
}

type GameConfig struct {
	HiddenUntilTurn int
	MaxTurns        int
	TurnDurationSec int
	Units           map[string]UnitConfig
	Regions         map[string]RegionConfig
	Paths           map[string]PathConfig
}

func Load(unitsPath, mapPath string) (*GameConfig, error) {
	uf, err := loadUnitsFile(unitsPath)
	if err != nil {
		return nil, err
	}
	mf, err := loadMapFile(mapPath)
	if err != nil {
		return nil, err
	}

	cfg := &GameConfig{
		HiddenUntilTurn: uf.HiddenUntilTurn,
		MaxTurns:        uf.MaxTurns,
		TurnDurationSec: uf.TurnDurationSec,
		Units:           make(map[string]UnitConfig, len(uf.Units)),
		Regions:         make(map[string]RegionConfig, len(mf.Regions)),
		Paths:           make(map[string]PathConfig, len(mf.Paths)),
	}
	for _, u := range uf.Units {
		cfg.Units[u.ID] = u
	}
	for _, r := range mf.Regions {
		cfg.Regions[r.ID] = r
	}
	for _, p := range mf.Paths {
		cfg.Paths[p.ID] = p
	}
	return cfg, nil
}

func loadUnitsFile(path string) (*UnitsFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var uf UnitsFile
	return &uf, json.NewDecoder(f).Decode(&uf)
}

func loadMapFile(path string) (*MapFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var mf MapFile
	return &mf, json.NewDecoder(f).Decode(&mf)
}
