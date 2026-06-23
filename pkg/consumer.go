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

// CreateConsumerFromConfig creates a consumer from a pre-built ConfigMap.
func CreateConsumerFromConfig(ccfg *kafka.ConfigMap) (*kafka.Consumer, error) {
	consumer, err := kafka.NewConsumer(ccfg)
	if err != nil {
		return nil, err
	}
	return consumer, nil
}

// ScanActiveSchemas scans all messages in a topic and extracts schema IDs
// from both the magic byte prefix in payloads AND from message headers
// (for topics using HeaderSchemaIdSerializer).
func ScanActiveSchemas(consumer *kafka.Consumer, topic string) (map[int32]int, error) {
	return scanActiveSchemas(consumer, topic)
}

// scanPollTimeoutMs is how long each Poll waits for a message or EOF event.
// scanDeadline caps the total time spent on a single topic as a backstop in
// case partition EOF is never reported.
const (
	scanPollTimeoutMs = 5000
	scanDeadline      = 5 * time.Minute
)

func scanActiveSchemas(consumer *kafka.Consumer, topic string) (map[int32]int, error) {
	activeSchemas := make(map[int32]int)
	metadata, err := consumer.GetMetadata(&topic, false, 5000)
	if err != nil {
		return nil, err
	}
	numPartitions := int32(len(metadata.Topics[topic].Partitions))
	if numPartitions == 0 {
		return nil, fmt.Errorf("no partition metadata returned (topic may not exist)")
	}

	topicName := topic
	tpl := make([]kafka.TopicPartition, 0, numPartitions)
	var totalMsg int64
	for i := int32(0); i < numPartitions; i++ {
		low, high, err := consumer.QueryWatermarkOffsets(topic, i, 10000)
		if err != nil {
			return nil, err
		}
		totalMsg += high - low
		tpl = append(tpl, kafka.TopicPartition{
			Topic:     &topicName,
			Partition: i,
			Offset:    kafka.OffsetBeginning,
		})
	}

	if totalMsg == 0 {
		fmt.Printf("Topic %s: no messages found, skipping.\n", topic)
		return activeSchemas, nil
	}

	if err := consumer.Assign(tpl); err != nil {
		return nil, err
	}

	// Terminate when every partition reports EOF rather than by comparing the
	// last consumed offset to the high watermark. The offset just below the
	// watermark can be a transaction control record or removed by compaction;
	// such offsets are never delivered to the consumer, so an offset comparison
	// would never reach the watermark and the scan would hang. Partition EOF
	// requires enable.partition.eof=true on the consumer config.
	//
	// The deadline backstops the case where EOF is never reported (e.g. a
	// partition stuck behind a persistent non-fatal error). It returns an error
	// so the caller marks the topic unverified instead of mistaking an
	// incomplete scan for a complete one and deleting in-use schemas.
	deadline := time.Now().Add(scanDeadline)
	eofPartitions := make(map[int32]bool)
	for !allPartitionsReachedEOF(eofPartitions, numPartitions) {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("scan deadline exceeded after %s", scanDeadline)
		}
		if err := handlePollEvent(consumer.Poll(scanPollTimeoutMs), topic, activeSchemas, eofPartitions); err != nil {
			return nil, err
		}
	}

	return activeSchemas, nil
}

// handlePollEvent records schema IDs from a polled message and tracks partitions
// that have reached EOF. It returns an error only for a fatal kafka.Error;
// non-fatal errors are logged and skipped, and a nil event (poll timeout) is a no-op.
func handlePollEvent(event kafka.Event, topic string, activeSchemas map[int32]int, eofPartitions map[int32]bool) error {
	switch e := event.(type) {
	case *kafka.Message:
		extractSchemaIDFromPayload(e.Value, VALUEONLY, activeSchemas)
		extractSchemaIDFromPayload(e.Key, KEYONLY, activeSchemas)
		extractSchemaIDsFromHeaders(e.Headers, activeSchemas)
	case kafka.PartitionEOF:
		eofPartitions[e.Partition] = true
	case kafka.Error:
		if e.IsFatal() {
			return e
		}
		fmt.Printf("%swarning: topic %s: %v%s\n", YELLOW, topic, e, RESET)
	}
	return nil
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

func allPartitionsReachedEOF(eofPartitions map[int32]bool, partitions int32) bool {
	for i := int32(0); i < partitions; i++ {
		if !eofPartitions[i] {
			return false
		}
	}
	return true
}
