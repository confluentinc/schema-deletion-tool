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
		"bootstrap.servers": "localhost:9092",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create producer: %s\n", err)
		os.Exit(1)
	}
	defer producer.Close()

	// Produce messages to "orders" topic using schema IDs 2 (v1) and 4 (v3)
	// Schema ID 3 (v2) is NOT used — should be a deletion candidate
	produceMessage(producer, "orders", 2)  // orders-value v1
	produceMessage(producer, "orders", 2)
	produceMessage(producer, "orders", 4)  // orders-value v3
	produceMessage(producer, "orders", 4)

	// Produce messages to "payments" topic using schema ID 5 (payments-value v1)
	// This makes payments-value v1 ACTIVE
	produceMessage(producer, "payments", 5)

	// Do NOT produce anything to "users" topic — users-value v1 should be candidate
	// Do NOT produce anything with schema ID 1 (address-value) — should be candidate

	producer.Flush(5000)
	fmt.Println("All test messages produced successfully.")
	fmt.Println("  orders topic: schema IDs 2 (v1) and 4 (v3) used")
	fmt.Println("  payments topic: schema ID 5 used")
	fmt.Println("  users topic: no messages")
	fmt.Println("")
	fmt.Println("Expected unused schemas:")
	fmt.Println("  address-value v1 (ID 1) - unused")
	fmt.Println("  orders-value v2 (ID 3) - unused")
	fmt.Println("  users-value v1 (ID 6) - unused")
	fmt.Println("  :.staging:orders-value v1 (ID 1) - unused (same ID as address-value)")
}

func produceMessage(producer *kafka.Producer, topic string, schemaID int) {
	// Build Confluent wire format: magic byte (0) + 4-byte schema ID + payload
	value := make([]byte, 10)
	value[0] = 0 // magic byte
	binary.BigEndian.PutUint32(value[1:5], uint32(schemaID))
	// Remaining bytes are dummy payload
	value[5] = 0x01
	value[6] = 0x02
	value[7] = 0x03
	value[8] = 0x04
	value[9] = 0x05

	deliveryChan := make(chan kafka.Event)
	err := producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          value,
	}, deliveryChan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to produce to %s: %s\n", topic, err)
		return
	}

	e := <-deliveryChan
	m := e.(*kafka.Message)
	if m.TopicPartition.Error != nil {
		fmt.Fprintf(os.Stderr, "Delivery failed for topic %s: %s\n", topic, m.TopicPartition.Error)
	} else {
		fmt.Printf("Produced to %s partition %d offset %v (schema ID %d)\n",
			topic, m.TopicPartition.Partition, m.TopicPartition.Offset, schemaID)
	}
}
