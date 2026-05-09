package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/rotr/option-b/internal/cache"
	"github.com/rotr/option-b/internal/graph"
)

// UnitInterceptPlan holds the interception plan for a single unit
type UnitInterceptPlan struct {
	UnitID       string  `json:"unitId"`
	TargetRegion string  `json:"targetRegion"`
	Score        float64 `json:"score"`
}

// InterceptPlan is the output of Pipeline 2
type InterceptPlan struct {
	ByUnit []UnitInterceptPlan `json:"byUnit"`
}

type nazgulRoutePair struct {
	nazgulID  string
	routeIdx  int
	routeName string
	routeRegs []string
}

// RunInterceptionPipeline runs the 4-worker interception analysis pipeline
func RunInterceptionPipeline(ctx context.Context, cacheSnap cache.WorldStateCache, g *graph.Graph, pathCfgMap map[string][2]string) InterceptPlan {
	// Gather all Nazgul
	var nazgulIDs []string
	for uid, uc := range cacheSnap.UnitConfigs {
		if uc.Class == "Nazgul" {
			if u, ok := cacheSnap.Units[uid]; ok && u.Status == "ACTIVE" {
				nazgulIDs = append(nazgulIDs, uid)
			}
		}
	}

	type work struct {
		pair nazgulRoutePair
	}
	type result struct {
		unitID       string
		targetRegion string
		score        float64
	}

	workCh := make(chan work, 30)
	resultCh := make(chan result)
	var wg sync.WaitGroup

	// 4 workers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case w, ok := <-workCh:
					if !ok {
						return
					}
					r := computeInterception(w.pair, cacheSnap, g, pathCfgMap)
					select {
					case resultCh <- r:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Dispatcher
	go func() {
		defer close(workCh)
		for _, nid := range nazgulIDs {
			for i, kr := range KnownRoutes {
				select {
				case workCh <- work{pair: nazgulRoutePair{
					nazgulID:  nid,
					routeIdx:  i,
					routeName: kr.Name,
					routeRegs: kr.RegionSeq,
				}}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Aggregator: best score per Nazgul
	bestByUnit := make(map[string]result)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				goto finalize
			}
			if existing, found := bestByUnit[r.unitID]; !found || r.score > existing.score {
				bestByUnit[r.unitID] = r
			}
		case <-timeout:
			goto finalize
		case <-ctx.Done():
			goto finalize
		}
	}

finalize:
	var plans []UnitInterceptPlan
	for uid, r := range bestByUnit {
		plans = append(plans, UnitInterceptPlan{
			UnitID:       uid,
			TargetRegion: r.targetRegion,
			Score:        r.score,
		})
	}
	return InterceptPlan{ByUnit: plans}
}

func computeInterception(pair nazgulRoutePair, cacheSnap cache.WorldStateCache, g *graph.Graph, pathCfgMap map[string][2]string) struct {
	unitID       string
	targetRegion string
	score        float64
} {
	nUnit, ok := cacheSnap.Units[pair.nazgulID]
	if !ok {
		return struct {
			unitID       string
			targetRegion string
			score        float64
		}{unitID: pair.nazgulID}
	}

	routeLength := float64(len(pair.routeRegs) - 1)
	bestScore := 0.0
	bestTarget := ""

	// For each region in the route, compute interception score
	rbTurns := 0 // turns for Ring Bearer to reach each region from start
	for i, reg := range pair.routeRegs {
		if i == 0 {
			continue
		}
		// Count path cost from previous region to this region
		from := pair.routeRegs[i-1]
		pathCost := 1
		for pid, endpoints := range pathCfgMap {
			if (endpoints[0] == from && endpoints[1] == reg) || (endpoints[0] == reg && endpoints[1] == from) {
				_ = pid
				pathCost = 1
				break
			}
		}
		rbTurns += pathCost

		turnsToIntercept := g.ShortestPath(nUnit.Region, reg)
		interceptWindow := rbTurns - turnsToIntercept

		var score float64
		if interceptWindow >= 0 && routeLength > 0 {
			score = 1.0 - (float64(turnsToIntercept) / routeLength)
			if score < 0 {
				score = 0
			}
		}

		if score > bestScore {
			bestScore = score
			bestTarget = reg
		}
	}

	return struct {
		unitID       string
		targetRegion string
		score        float64
	}{unitID: pair.nazgulID, targetRegion: bestTarget, score: bestScore}
}
