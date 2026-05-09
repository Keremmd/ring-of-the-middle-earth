package kafka

import (
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

const (
	TopicOrdersRaw       = "game.orders.raw"
	TopicOrdersValidated = "game.orders.validated"
	TopicEventsUnit      = "game.events.unit"
	TopicEventsRegion    = "game.events.region"
	TopicEventsPath      = "game.events.path"
	TopicSession         = "game.session"
	TopicBroadcast       = "game.broadcast"
	TopicRingPosition    = "game.ring.position"
	TopicRingDetection   = "game.ring.detection"
	TopicDLQ             = "game.dlq"
)

// CreateTopics creates all required Kafka topics
func CreateTopics(brokers string) error {
	adminClient, err := kafka.NewAdminClient(&kafka.ConfigMap{
		"bootstrap.servers": brokers,
	})
	if err != nil {
		return fmt.Errorf("admin client: %w", err)
	}
	defer adminClient.Close()

	topicSpecs := []kafka.TopicSpecification{
		{Topic: TopicOrdersRaw, NumPartitions: 3, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "3600000", "cleanup.policy": "delete"}},
		{Topic: TopicOrdersValidated, NumPartitions: 6, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "3600000", "cleanup.policy": "delete"}},
		{Topic: TopicEventsUnit, NumPartitions: 6, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete"}},
		{Topic: TopicEventsRegion, NumPartitions: 6, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete"}},
		{Topic: TopicEventsPath, NumPartitions: 6, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete"}},
		{Topic: TopicSession, NumPartitions: 1, ReplicationFactor: 1,
			Config: map[string]string{"cleanup.policy": "compact"}},
		{Topic: TopicBroadcast, NumPartitions: 1, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "3600000", "cleanup.policy": "delete"}},
		{Topic: TopicRingPosition, NumPartitions: 1, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "3600000", "cleanup.policy": "delete"}},
		{Topic: TopicRingDetection, NumPartitions: 2, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "3600000", "cleanup.policy": "delete"}},
		{Topic: TopicDLQ, NumPartitions: 3, ReplicationFactor: 1,
			Config: map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete"}},
	}

	results, err := adminClient.CreateTopics(nil, topicSpecs)
	if err != nil {
		return fmt.Errorf("create topics: %w", err)
	}
	for _, r := range results {
		if r.Error.Code() != kafka.ErrNoError && r.Error.Code() != kafka.ErrTopicAlreadyExists {
			fmt.Printf("topic %s: %v\n", r.Topic, r.Error)
		}
	}
	return nil
}
