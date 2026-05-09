package tests

import (
	"encoding/json"
	"testing"

	"github.com/rotr/option-b/internal/cache"
	"github.com/rotr/option-b/internal/config"
	"github.com/rotr/option-b/internal/game"
	kafkaclient "github.com/rotr/option-b/internal/kafka"
	"github.com/rotr/option-b/internal/router"
)

// router_test.go — 3 required cases per Section 35 (Option B)
// All cases must pass with go test -race

// Case 1: WorldStateSnapshot with ring-bearer region set
// → Dark Side receives currentRegion="", Light Side receives real value
func TestRouter_RingBearerStripped(t *testing.T) {
	t.Parallel()

	er := router.NewEventRouter()

	// Build a WorldStateSnapshot with ring-bearer region exposed
	snapshot := map[string]interface{}{
		"turn": 5,
		"units": []map[string]interface{}{
			{"id": "ring-bearer", "region": "weathertop", "strength": 1, "status": "ACTIVE"},
			{"id": "aragorn", "region": "bree", "strength": 5, "status": "ACTIVE"},
		},
		"timestamp": 1000000,
	}
	payload, _ := json.Marshal(snapshot)

	msg := &kafkaclient.Message{
		Topic: kafkaclient.TopicBroadcast,
		Key:   "world",
		Value: payload,
	}

	er.Route(msg)

	// Light Side should receive full event
	select {
	case lightEvent := <-er.LightSideSSECh:
		var lightSnap map[string]interface{}
		json.Unmarshal(lightEvent.Payload, &lightSnap)
		units := lightSnap["units"].([]interface{})
		for _, u := range units {
			unitMap := u.(map[string]interface{})
			if unitMap["id"] == "ring-bearer" {
				if unitMap["region"] != "weathertop" {
					t.Errorf("Case 1: Light Side should see ring-bearer at 'weathertop', got '%v'", unitMap["region"])
				}
			}
		}
	default:
		t.Error("Case 1: Light Side SSE channel should have received event")
	}

	// Dark Side should receive stripped event
	select {
	case darkEvent := <-er.DarkSideSSECh:
		var darkSnap map[string]interface{}
		json.Unmarshal(darkEvent.Payload, &darkSnap)
		units := darkSnap["units"].([]interface{})
		for _, u := range units {
			unitMap := u.(map[string]interface{})
			if unitMap["id"] == "ring-bearer" {
				if unitMap["region"] != "" {
					t.Errorf("Case 1: Dark Side should receive ring-bearer.region='', got '%v'", unitMap["region"])
				}
			}
		}
	default:
		t.Error("Case 1: Dark Side SSE channel should have received event")
	}
}

// Case 2: RingBearerMoved event → never reaches Dark Side SSE channel
func TestRouter_RingBearerMovedNeverToDark(t *testing.T) {
	t.Parallel()

	er := router.NewEventRouter()

	payload := []byte(`{"trueRegion":"rivendell","turn":3,"timestamp":999}`)
	msg := &kafkaclient.Message{
		Topic: kafkaclient.TopicRingPosition,
		Key:   "ring-bearer",
		Value: payload,
	}

	er.Route(msg)

	// Light Side MUST receive it
	select {
	case lightEvent := <-er.LightSideSSECh:
		if lightEvent.Topic != kafkaclient.TopicRingPosition {
			t.Errorf("Case 2: Light Side should receive ring.position event")
		}
	default:
		t.Error("Case 2: Light Side SSE should have received RingBearerMoved")
	}

	// Dark Side MUST NOT receive it
	select {
	case <-er.DarkSideSSECh:
		t.Error("Case 2: RingBearerMoved must NEVER reach Dark Side SSE channel")
	default:
		// Correct — channel is empty
	}
}

// Case 3: cache.DarkView.RingBearerRegion is always "" after any cache update
func TestRouter_DarkViewAlwaysEmpty(t *testing.T) {
	t.Parallel()

	unitCfgs := map[string]config.UnitConfig{
		"ring-bearer": {ID: "ring-bearer", Class: "RingBearer"},
	}
	wsc := cache.NewWorldStateCache(unitCfgs)

	// Initial state
	if wsc.DarkView.RingBearerRegion != "" {
		t.Errorf("Case 3: DarkView.RingBearerRegion must be '' on init, got '%s'", wsc.DarkView.RingBearerRegion)
	}

	// After a world state update with a real ring bearer region
	snap := game.WorldStateSnapshot{
		Turn: 7,
		Units: []*game.UnitSnapshot{
			{ID: "ring-bearer", Region: "moria", Strength: 1, Status: game.StatusActive},
		},
		Regions: []*game.RegionState{},
	}
	wsc.Update(snap, "moria", []string{"rivendell-to-moria"}, 1)

	// DarkView MUST still be ""
	if wsc.DarkView.RingBearerRegion != "" {
		t.Errorf("Case 3: DarkView.RingBearerRegion must be '' after Update, got '%s'", wsc.DarkView.RingBearerRegion)
	}

	// LightView should have the real region
	snap2 := wsc.Snapshot()
	if snap2.LightView.RingBearerRegion != "moria" {
		t.Errorf("Case 3: LightView.RingBearerRegion should be 'moria', got '%s'", snap2.LightView.RingBearerRegion)
	}

	// Snapshot's DarkView must also be ""
	if snap2.DarkView.RingBearerRegion != "" {
		t.Errorf("Case 3: Snapshot DarkView.RingBearerRegion must be '', got '%s'", snap2.DarkView.RingBearerRegion)
	}

	// After detection update
	wsc.UpdateDetection("rivendell", 7)
	if wsc.DarkView.RingBearerRegion != "" {
		t.Errorf("Case 3: DarkView.RingBearerRegion must be '' after UpdateDetection, got '%s'", wsc.DarkView.RingBearerRegion)
	}
	if wsc.DarkView.LastDetectedRegion != "rivendell" {
		t.Errorf("Case 3: DarkView.LastDetectedRegion should be 'rivendell', got '%s'", wsc.DarkView.LastDetectedRegion)
	}
}

// Race condition test: concurrent cache updates must not leak ring bearer region
func TestRouter_DarkViewRaceCondition(t *testing.T) {
	unitCfgs := map[string]config.UnitConfig{
		"ring-bearer": {ID: "ring-bearer", Class: "RingBearer"},
	}
	wsc := cache.NewWorldStateCache(unitCfgs)

	done := make(chan struct{})

	// Writer goroutine: update cache with ring bearer in various regions
	go func() {
		regions := []string{"the-shire", "bree", "weathertop", "rivendell", "moria", "lothlorien"}
		for i := 0; i < 100; i++ {
			reg := regions[i%len(regions)]
			snap := game.WorldStateSnapshot{
				Turn: i,
				Units: []*game.UnitSnapshot{
					{ID: "ring-bearer", Region: reg, Strength: 1, Status: game.StatusActive},
				},
				Regions: []*game.RegionState{},
			}
			wsc.Update(snap, reg, nil, 0)
		}
		close(done)
	}()

	// Reader goroutine: verify DarkView is always ""
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				snap := wsc.Snapshot()
				if snap.DarkView.RingBearerRegion != "" {
					t.Errorf("RACE: DarkView.RingBearerRegion leaked: '%s'", snap.DarkView.RingBearerRegion)
				}
			}
		}
	}()

	<-done
}
