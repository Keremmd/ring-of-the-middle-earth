package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/rotr/option-b/internal/cache"
	"github.com/rotr/option-b/internal/graph"
)

// RankedRoute holds the risk analysis for a single route
type RankedRoute struct {
	Name         string   `json:"name"`
	PathIDs      []string `json:"pathIds"`
	RegionIDs    []string `json:"regionIds"`
	RiskScore    int      `json:"riskScore"`
	ThreatPaths  []string `json:"threatenedPaths"`
	BlockedPaths []string `json:"blockedPaths"`
	Warnings     []string `json:"warnings"`
}

// RankedRouteList is the output of Pipeline 1
type RankedRouteList struct {
	Routes      []RankedRoute `json:"routes"`
	Recommended string        `json:"recommended"`
	Warnings    []string      `json:"warnings"`
}

// KnownRoutes are the four canonical routes from the spec
var KnownRoutes = []struct {
	Name      string
	RegionSeq []string
}{
	{
		Name:      "Fellowship (Route 1)",
		RegionSeq: []string{"the-shire", "bree", "weathertop", "rivendell", "moria", "lothlorien", "emyn-muil", "ithilien", "cirith-ungol", "mount-doom"},
	},
	{
		Name:      "Northern Bypass (Route 2)",
		RegionSeq: []string{"the-shire", "bree", "rivendell", "lothlorien", "emyn-muil", "dead-marshes", "ithilien", "cirith-ungol", "mount-doom"},
	},
	{
		Name:      "Dark Route (Route 3)",
		RegionSeq: []string{"the-shire", "bree", "rivendell", "lothlorien", "emyn-muil", "dead-marshes", "mordor", "mount-doom"},
	},
	{
		Name:      "Southern Corridor (Route 4)",
		RegionSeq: []string{"the-shire", "tharbad", "fords-of-isen", "edoras", "minas-tirith", "osgiliath", "minas-morgul", "cirith-ungol", "mount-doom"},
	},
}

// RunRouteRiskPipeline runs the 4-worker pipeline for route risk analysis
func RunRouteRiskPipeline(ctx context.Context, cacheSnap cache.WorldStateCache, g *graph.Graph, pathCfgMap map[string][2]string) RankedRouteList {
	type work struct {
		idx  int
		name string
		regs []string
	}
	type result struct {
		idx   int
		route RankedRoute
	}

	workCh := make(chan work, 20)
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
					r := computeRouteRiskWithPaths(w.regs, cacheSnap, g, pathCfgMap)
					r.Name = w.name
					select {
					case resultCh <- result{idx: w.idx, route: r}:
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
		for i, kr := range KnownRoutes {
			select {
			case workCh <- work{idx: i, name: kr.Name, regs: kr.RegionSeq}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Aggregator with timeout
	routes := make([]RankedRoute, len(KnownRoutes))
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	timeout := time.After(2 * time.Second)
	collected := 0
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				goto finalize
			}
			routes[r.idx] = r.route
			collected++
			if collected == len(KnownRoutes) {
				goto finalize
			}
		case <-timeout:
			goto finalize
		case <-ctx.Done():
			goto finalize
		}
	}

finalize:
	// Sort by risk score ascending (lowest risk = best route)
	for i := 0; i < len(routes)-1; i++ {
		for j := i + 1; j < len(routes); j++ {
			if routes[j].RiskScore < routes[i].RiskScore {
				routes[i], routes[j] = routes[j], routes[i]
			}
		}
	}

	recommended := ""
	if len(routes) > 0 {
		recommended = routes[0].Name
	}

	return RankedRouteList{Routes: routes, Recommended: recommended}
}

func computeRouteRiskWithPaths(regions []string, cacheSnap cache.WorldStateCache, g *graph.Graph, pathCfgMap map[string][2]string) RankedRoute {
	route := RankedRoute{RegionIDs: regions}

	var threatened, blocked []string
	regionThreat := 0
	pathSurveillance := 0

	for _, rid := range regions[1:] {
		if r, ok := cacheSnap.Regions[rid]; ok {
			regionThreat += r.ThreatLevel
		}
	}

	for i := 0; i < len(regions)-1; i++ {
		from := regions[i]
		to := regions[i+1]
		for pid, endpoints := range pathCfgMap {
			if (endpoints[0] == from && endpoints[1] == to) || (endpoints[0] == to && endpoints[1] == from) {
				if ps, ok := cacheSnap.Paths[pid]; ok {
					pathSurveillance += ps.SurveillanceLevel
					switch ps.Status {
					case "THREATENED":
						threatened = append(threatened, pid)
					case "BLOCKED":
						blocked = append(blocked, pid)
					}
				}
				break
			}
		}
	}

	nazgulProximity := 0
	for uid, u := range cacheSnap.Units {
		uc, ok := cacheSnap.UnitConfigs[uid]
		if !ok || uc.Class != "Nazgul" || u.Status != "ACTIVE" {
			continue
		}
		for _, rid := range regions {
			if g.Distance(u.Region, rid) <= 2 {
				nazgulProximity++
				break
			}
		}
	}

	score := regionThreat +
		pathSurveillance*3 +
		len(threatened)*2 +
		len(blocked)*5 +
		nazgulProximity*2

	route.RiskScore = score
	route.ThreatPaths = threatened
	route.BlockedPaths = blocked

	if len(blocked) > 0 {
		route.Warnings = append(route.Warnings, "Route contains blocked paths")
	}
	if nazgulProximity > 0 {
		route.Warnings = append(route.Warnings, "Nazgul nearby")
	}
	return route
}
