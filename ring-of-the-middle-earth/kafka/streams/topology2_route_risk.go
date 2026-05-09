// Topology 2: Route Risk Enrichment
// Source: game.orders.validated — filter ASSIGN_ROUTE and REDIRECT_UNIT
// KTables: PathKTable, RegionKTable
//
// Enriches orders with routeRiskScore, threatenedPaths[], blockedPaths[]
// and re-emits enriched record back to game.orders.validated
package streams

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// RouteRiskTopology implements Kafka Streams Topology 2
type RouteRiskTopology struct {
	producer    *kafka.Producer
	brokers     string
	pathStore   *KTable
	regionStore *KTable
	unitStore   *KTable
}

// NewRouteRiskTopology creates a new route risk enrichment topology
func NewRouteRiskTopology(brokers string) (*RouteRiskTopology, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"enable.idempotence": true,
		"acks":               "all",
	})
	if err != nil {
		return nil, err
	}

	return &RouteRiskTopology{
		producer:    p,
		brokers:     brokers,
		pathStore:   newKTable(),
		regionStore: newKTable(),
		unitStore:   newKTable(),
	}, nil
}

// Run starts the topology
func (t *RouteRiskTopology) Run() {
	go t.updateKTable(t.brokers, "game.events.path", "topology2-path-reader", t.pathStore)
	go t.updateKTable(t.brokers, "game.events.region", "topology2-region-reader", t.regionStore)
	go t.updateKTable(t.brokers, "game.events.unit", "topology2-unit-reader", t.unitStore)

	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": t.brokers,
		"group.id":          "topology2-enricher",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		fmt.Printf("topology2 consumer error: %v\n", err)
		return
	}
	defer consumer.Close()
	consumer.Subscribe("game.orders.validated", nil)

	for {
		ev := consumer.Poll(100)
		msg, ok := ev.(*kafka.Message)
		if !ok {
			continue
		}

		var order map[string]interface{}
		if err := json.Unmarshal(msg.Value, &order); err != nil {
			continue
		}

		orderType, _ := order["orderType"].(string)
		if orderType != "ASSIGN_ROUTE" && orderType != "REDIRECT_UNIT" {
			continue
		}

		// Extract path IDs
		var pathIDs []string
		if ids, ok := order["pathIds"].([]interface{}); ok {
			for _, id := range ids {
				if s, ok := id.(string); ok {
					pathIDs = append(pathIDs, s)
				}
			}
		}
		if ids, ok := order["newPathIds"].([]interface{}); ok {
			for _, id := range ids {
				if s, ok := id.(string); ok {
					pathIDs = append(pathIDs, s)
				}
			}
		}

		riskScore, threatened, blocked := t.computeRisk(pathIDs)

		order["routeRiskScore"] = riskScore
		order["threatenedPaths"] = threatened
		order["blockedPaths"] = blocked
		order["enrichedAt"] = time.Now().UnixMilli()

		enriched, _ := json.Marshal(order)
		topic := "game.orders.validated"
		t.producer.Produce(&kafka.Message{
			TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
			Key:            msg.Key,
			Value:          enriched,
		}, nil)
	}
}

type pathStateData struct {
	Status            string `json:"status"`
	SurveillanceLevel int    `json:"surveillanceLevel"`
}

type regionStateData struct {
	ThreatLevel  int    `json:"threatLevel"`
	ControlledBy string `json:"controlledBy"`
}

type unitStateData struct {
	Region string `json:"region"`
	Status string `json:"status"`
	Class  string `json:"class"`
}

func (t *RouteRiskTopology) computeRisk(pathIDs []string) (int, []string, []string) {
	var threatened, blocked []string
	surveillanceSum := 0
	regionThreat := 0
	threatCount := 0
	blockedCount := 0

	for _, pid := range pathIDs {
		raw, ok := t.pathStore.Get(pid)
		if !ok {
			continue
		}
		var ps pathStateData
		if err := json.Unmarshal(raw, &ps); err != nil {
			continue
		}
		surveillanceSum += ps.SurveillanceLevel
		switch ps.Status {
		case "THREATENED":
			threatened = append(threatened, pid)
			threatCount++
		case "BLOCKED":
			blocked = append(blocked, pid)
			blockedCount++
		}
	}

	// Region threat — from destination regions
	t.regionStore.mu.RLock()
	for _, rRaw := range t.regionStore.data {
		var rs regionStateData
		if err := json.Unmarshal(rRaw, &rs); err == nil {
			regionThreat += rs.ThreatLevel
		}
	}
	t.regionStore.mu.RUnlock()

	// Nazgul proximity
	nazgulProximity := 0
	t.unitStore.mu.RLock()
	for _, uRaw := range t.unitStore.data {
		var us unitStateData
		if err := json.Unmarshal(uRaw, &us); err == nil {
			if us.Class == "Nazgul" && us.Status == "ACTIVE" {
				nazgulProximity++
			}
		}
	}
	t.unitStore.mu.RUnlock()

	score := regionThreat +
		surveillanceSum*3 +
		threatCount*2 +
		blockedCount*5 +
		nazgulProximity*2

	return score, threatened, blocked
}

func (t *RouteRiskTopology) updateKTable(brokers, topic, groupID string, store *KTable) {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
		"group.id":          groupID,
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		fmt.Printf("ktable consumer error for %s: %v\n", topic, err)
		return
	}
	defer consumer.Close()
	consumer.Subscribe(topic, nil)
	for {
		ev := consumer.Poll(100)
		if msg, ok := ev.(*kafka.Message); ok {
			store.Put(string(msg.Key), json.RawMessage(msg.Value))
		}
	}
}
