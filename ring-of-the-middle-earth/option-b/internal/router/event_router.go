package router

import (
	"encoding/json"

	"github.com/rotr/option-b/internal/kafka"
)

// Event is a routed game event
type Event struct {
	Topic   string
	Payload []byte
}

// EventRouter routes Kafka events to the correct SSE channels.
// It is the single enforcement point for information asymmetry.
type EventRouter struct {
	LightSideSSECh chan Event
	DarkSideSSECh  chan Event
	CacheUpdateCh  chan Event
	EngineCh       chan Event
}

// NewEventRouter creates a new EventRouter with buffered channels
func NewEventRouter() *EventRouter {
	return &EventRouter{
		LightSideSSECh: make(chan Event, 100),
		DarkSideSSECh:  make(chan Event, 100),
		CacheUpdateCh:  make(chan Event, 100),
		EngineCh:       make(chan Event, 100),
	}
}

// Route dispatches a Kafka message to the appropriate channels
func (r *EventRouter) Route(msg *kafka.Message) {
	event := Event{Topic: msg.Topic, Payload: msg.Value}

	switch msg.Topic {
	case kafka.TopicRingPosition:
		// Light Side only — NEVER to Dark Side
		r.LightSideSSECh <- event

	case kafka.TopicRingDetection:
		// Dark Side only — NEVER to Light Side
		r.DarkSideSSECh <- event
		r.CacheUpdateCh <- event

	case kafka.TopicBroadcast:
		// Light Side gets full event
		r.LightSideSSECh <- event
		// Dark Side gets ring-bearer stripped
		r.DarkSideSSECh <- Event{Topic: msg.Topic, Payload: stripRingBearer(msg.Value)}
		r.CacheUpdateCh <- event

	case kafka.TopicEventsUnit, kafka.TopicEventsRegion, kafka.TopicEventsPath:
		r.LightSideSSECh <- event
		r.DarkSideSSECh <- event
		r.CacheUpdateCh <- event

	case kafka.TopicOrdersValidated:
		r.EngineCh <- event
	}
}

// stripRingBearer removes the Ring Bearer's position from a WorldStateSnapshot payload
func stripRingBearer(payload []byte) []byte {
	var snapshot map[string]interface{}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}

	units, ok := snapshot["units"].([]interface{})
	if !ok {
		return payload
	}

	for i, unitRaw := range units {
		unit, ok := unitRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if unit["id"] == "ring-bearer" {
			unit["region"] = "" // ENFORCED: never expose true region
			units[i] = unit
		}
	}
	snapshot["units"] = units

	result, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return result
}
