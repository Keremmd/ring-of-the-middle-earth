package kafka

import (
	"encoding/json"
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Producer wraps the Kafka producer
type Producer struct {
	p *kafka.Producer
}

// NewProducer creates a new Kafka producer
func NewProducer(brokers string) (*Producer, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"enable.idempotence": true,
		"acks":               "all",
		"retries":            5,
	})
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					fmt.Printf("kafka delivery error: %v\n", ev.TopicPartition.Error)
				}
			}
		}
	}()
	return &Producer{p: p}, nil
}

// Produce sends a message to the given topic with the given key
func (p *Producer) Produce(topic, key string, value interface{}) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return p.p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Value:          bytes,
	}, nil)
}

// Flush waits for all outstanding messages to be delivered
func (p *Producer) Flush(timeoutMs int) {
	p.p.Flush(timeoutMs)
}

// Close shuts down the producer
func (p *Producer) Close() {
	p.p.Close()
}
