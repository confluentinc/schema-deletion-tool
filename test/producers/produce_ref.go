//go:build ignore

package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func main() {
	producer, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": "127.0.0.1:9092",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create producer: %s\n", err)
		os.Exit(1)
	}
	defer producer.Close()

	// Produce messages to "order" topic using schema ID 2 (order-value v1, which references address-value)
	produceMsg(producer, "order", 2)
	produceMsg(producer, "order", 2)

	// Produce messages to "payments" topic using schema ID 3 (payments-value v1)
	produceMsg(producer, "payments", 3)

	// Also produce to "order" topic with schema ID in HEADER (testing HeaderSchemaIdSerializer)
	produceWithHeader(producer, "order", 2)

	// Do NOT produce to "users" topic — users-value should be unused
	// Do NOT produce with address-value (ID 1) — but it's referenced by active order-value

	producer.Flush(5000)
	fmt.Println("Messages produced.")
	fmt.Println("Expected results:")
	fmt.Println("  address-value v1 (ID 1): BLOCKED - referenced by active order-value (ID 2)")
	fmt.Println("  order-value v1 (ID 2): ACTIVE - messages in 'order' topic")
	fmt.Println("  payments-value v1 (ID 3): ACTIVE - messages in 'payments' topic")
	fmt.Println("  users-value v1 (ID 4): SAFE to delete - no messages anywhere")
}

func produceMsg(producer *kafka.Producer, topic string, schemaID int) {
	value := make([]byte, 10)
	value[0] = 0 // magic byte
	binary.BigEndian.PutUint32(value[1:5], uint32(schemaID))
	value[5] = 0x01

	deliveryChan := make(chan kafka.Event)
	producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          value,
	}, deliveryChan)

	e := <-deliveryChan
	m := e.(*kafka.Message)
	if m.TopicPartition.Error != nil {
		fmt.Fprintf(os.Stderr, "Delivery failed: %s\n", m.TopicPartition.Error)
	} else {
		fmt.Printf("Produced to %s (magic byte, schema ID %d)\n", topic, schemaID)
	}
}

func produceWithHeader(producer *kafka.Producer, topic string, schemaID int) {
	// Clean payload (no magic byte prefix)
	value := []byte(`{"id":"order-123","amount":99.99}`)

	// Schema ID in header
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, uint32(schemaID))

	deliveryChan := make(chan kafka.Event)
	producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          value,
		Headers: []kafka.Header{
			{Key: "value.schema.id", Value: idBytes},
		},
	}, deliveryChan)

	e := <-deliveryChan
	m := e.(*kafka.Message)
	if m.TopicPartition.Error != nil {
		fmt.Fprintf(os.Stderr, "Delivery failed: %s\n", m.TopicPartition.Error)
	} else {
		fmt.Printf("Produced to %s (header, schema ID %d)\n", topic, schemaID)
	}
}
