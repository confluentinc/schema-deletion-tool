package pkg

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Default header names used by Confluent's HeaderSchemaIdSerializer.
// The serializer puts schema IDs in headers instead of the magic byte prefix.
var (
	DefaultValueSchemaIDHeaders = []string{"value.schema.id", "sr.value.schema.id", "schema.id"}
	DefaultKeySchemaIDHeaders   = []string{"key.schema.id", "sr.key.schema.id"}
)

func CreateConsumer(bootstrapServer string, credentials Credentials) (*kafka.Consumer, error) {
	ccfg, err := createConsumerConfig(bootstrapServer, credentials)
	if err != nil {
		return nil, err
	}
	consumer, err := kafka.NewConsumer(ccfg)
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

// CreateConsumerFromConfig creates a consumer from a pre-built ConfigMap.
func CreateConsumerFromConfig(ccfg *kafka.ConfigMap) (*kafka.Consumer, error) {
	consumer, err := kafka.NewConsumer(ccfg)
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

func createConsumerConfig(bootstrapServer string, credentials Credentials) (*kafka.ConfigMap, error) {
	ccfg := &kafka.ConfigMap{}
	if err := ccfg.SetKey("bootstrap.servers", bootstrapServer); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("sasl.mechanism", "PLAIN"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("security.protocol", "SASL_SSL"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("ssl.endpoint.identification.algorithm", "https"); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("sasl.username", credentials.ApiKey); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("sasl.password", credentials.ApiSecret); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("group.id", fmt.Sprintf("schema-deletion-tool-%d", time.Now().UnixNano())); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("enable.auto.commit", false); err != nil {
		return nil, err
	}
	if err := ccfg.SetKey("auto.offset.reset", "earliest"); err != nil {
		return nil, err
	}
	return ccfg, nil
}

// ScanActiveSchemas scans all messages in a topic and extracts schema IDs
// from both the magic byte prefix in payloads AND from message headers
// (for topics using HeaderSchemaIdSerializer).
func ScanActiveSchemas(consumer *kafka.Consumer, topic string) (map[int32]int, error) {
	return scanActiveSchemas(consumer, topic)
}

func scanActiveSchemas(consumer *kafka.Consumer, topic string) (map[int32]int, error) {
	activeSchemas := make(map[int32]int)
	metadata, err := consumer.GetMetadata(&topic, false, 5000)
	if err != nil {
		return nil, err
	}
	DesiredNumPartitions := int32(len(metadata.Topics[topic].Partitions))
	var tpl []kafka.TopicPartition
	var topicName = topic
	for i := int32(0); i < DesiredNumPartitions; i++ {
		tpl = append(tpl, kafka.TopicPartition{
			Topic:     &topicName,
			Partition: i,
			Offset:    kafka.OffsetBeginning,
		})
	}
	err = consumer.Assign(tpl)
	if err != nil {
		return nil, err
	}

	var totalMsg int64 = 0
	endOffsets := make(map[int32]kafka.Offset)
	offsets := make(map[int32]kafka.Offset)
	for i := int32(0); i < DesiredNumPartitions; i++ {
		low, high, err := consumer.QueryWatermarkOffsets(topic, i, 10000)
		if err != nil {
			return nil, err
		}
		endOffsets[i] = kafka.Offset(high) - 1
		offsets[i] = kafka.Offset(low) - 1
		totalMsg += high - low
	}

	if totalMsg == 0 {
		fmt.Println("No messages found, skipping...")
		return activeSchemas, nil
	}

	deadline := time.Now().Add(5 * time.Minute) // per-topic scan deadline
	consecutiveTimeouts := 0
	maxConsecutiveTimeouts := 3

	for !checkIfReachesOffsets(endOffsets, offsets, DesiredNumPartitions) {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("topic %s: scan deadline exceeded (5 minutes)", topic)
		}

		msg, err := consumer.ReadMessage(10 * time.Second)
		if err != nil {
			// ReadMessage timeout is not fatal — it means no message available within the timeout
			kafkaErr, ok := err.(kafka.Error)
			if ok && kafkaErr.Code() == kafka.ErrTimedOut {
				consecutiveTimeouts++
				if consecutiveTimeouts >= maxConsecutiveTimeouts {
					// After 3 consecutive timeouts (30s), assume we've read everything available
					break
				}
				continue
			}
			return nil, err
		}
		consecutiveTimeouts = 0
		offsets[msg.TopicPartition.Partition] = msg.TopicPartition.Offset

		// Extract schema IDs from payload (magic byte prefix)
		extractSchemaIDFromPayload(msg.Value, VALUEONLY, activeSchemas)
		extractSchemaIDFromPayload(msg.Key, KEYONLY, activeSchemas)

		// Extract schema IDs from headers (HeaderSchemaIdSerializer)
		extractSchemaIDsFromHeaders(msg.Headers, activeSchemas)
	}

	return activeSchemas, nil
}

// extractSchemaIDFromPayload extracts schema ID from the Confluent wire format:
// byte 0 = magic byte (0x00), bytes 1-4 = schema ID (big-endian).
func extractSchemaIDFromPayload(data []byte, schemaType int, activeSchemas map[int32]int) {
	if len(data) >= MessageOffset && data[0] == MagicByte {
		schemaID := int32(binary.BigEndian.Uint32(data[1:MessageOffset]))
		activeSchemas[schemaID] = activeSchemas[schemaID] | schemaType
	}
}

// extractSchemaIDsFromHeaders checks message headers for schema IDs placed by
// Confluent's HeaderSchemaIdSerializer. The schema ID can be stored as either:
// - 4-byte big-endian binary (standard Confluent format)
// - String-encoded integer (some client implementations)
func extractSchemaIDsFromHeaders(headers []kafka.Header, activeSchemas map[int32]int) {
	for _, h := range headers {
		headerKey := strings.ToLower(h.Key)
		schemaType := classifySchemaIDHeader(headerKey)
		if schemaType == 0 {
			continue
		}

		schemaID := parseSchemaIDFromHeaderValue(h.Value)
		if schemaID > 0 {
			activeSchemas[schemaID] = activeSchemas[schemaID] | schemaType
		}
	}
}

// classifySchemaIDHeader returns KEYONLY, VALUEONLY, or 0 based on the header name.
func classifySchemaIDHeader(headerKey string) int {
	for _, name := range DefaultKeySchemaIDHeaders {
		if headerKey == name {
			return KEYONLY
		}
	}
	for _, name := range DefaultValueSchemaIDHeaders {
		if headerKey == name {
			return VALUEONLY
		}
	}
	return 0
}

// parseSchemaIDFromHeaderValue parses a schema ID from header bytes.
// Supports both 4-byte big-endian binary and UTF-8 string-encoded integers.
func parseSchemaIDFromHeaderValue(value []byte) int32 {
	if len(value) == 0 {
		return 0
	}

	// Try 4-byte big-endian (standard Confluent format)
	if len(value) == 4 {
		return int32(binary.BigEndian.Uint32(value))
	}

	// Try string-encoded integer
	if id, err := strconv.ParseInt(string(value), 10, 32); err == nil {
		return int32(id)
	}

	return 0
}

func checkIfReachesOffsets(endOffsets, offsets map[int32]kafka.Offset, partitions int32) bool {
	for i := int32(0); i < partitions; i++ {
		val, ok := offsets[i]
		if !ok {
			val = -1
		}
		endVal, _ := endOffsets[i]
		if val < endVal {
			return false
		}
	}
	return true
}
