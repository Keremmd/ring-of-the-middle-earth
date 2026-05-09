package kafka

import (
	"encoding/json"
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Message is a consumed Kafka message
type Message struct {
	Topic string
	Key   string
	Value []byte
}

// Consumer wraps the Kafka consumer
type Consumer struct {
	c *kafka.Consumer
}

// NewConsumer creates a new Kafka consumer in the given group
func NewConsumer(brokers, groupID string, topics []string) (*Consumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"group.id":           groupID,
		"auto.offset.reset":  "earliest",
		"enable.auto.commit": true,
		"session.timeout.ms": 6000,
		"heartbeat.interval.ms": 2000,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka consumer: %w", err)
	}
	if err := c.SubscribeTopics(topics, nil); err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	return &Consumer{c: c}, nil
}

// Poll polls for a message with a timeout in milliseconds
func (c *Consumer) Poll(timeoutMs int) (*Message, error) {
	ev := c.c.Poll(timeoutMs)
	if ev == nil {
		return nil, nil
	}
	switch e := ev.(type) {
	case *kafka.Message:
		return &Message{
			Topic: *e.TopicPartition.Topic,
			Key:   string(e.Key),
			Value: e.Value,
		}, nil
	case kafka.Error:
		return nil, fmt.Errorf("kafka error: %w", e)
	}
	return nil, nil
}

// Unmarshal decodes the message value into the given target
func (m *Message) Unmarshal(target interface{}) error {
	return json.Unmarshal(m.Value, target)
}

// Close closes the consumer
func (c *Consumer) Close() {
	c.c.Close()
}
