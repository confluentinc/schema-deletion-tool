package pkg

import (
	"encoding/binary"
	"testing"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/require"
)

func TestAllPartitionsReachedEOF(t *testing.T) {
	req := require.New(t)
	// No partitions: vacuously done.
	req.True(allPartitionsReachedEOF(map[int32]bool{}, 0))
	// Single partition not yet at EOF.
	req.False(allPartitionsReachedEOF(map[int32]bool{}, 1))
	req.False(allPartitionsReachedEOF(map[int32]bool{0: false}, 1))
	// Single partition at EOF.
	req.True(allPartitionsReachedEOF(map[int32]bool{0: true}, 1))
	// Multi-partition: all must be at EOF.
	req.False(allPartitionsReachedEOF(map[int32]bool{0: true}, 2))
	req.False(allPartitionsReachedEOF(map[int32]bool{0: true, 1: false}, 2))
	req.True(allPartitionsReachedEOF(map[int32]bool{0: true, 1: true}, 2))
}

func TestHandlePollEvent_Message(t *testing.T) {
	req := require.New(t)
	active := make(map[int32]int)
	eof := make(map[int32]bool)

	value := make([]byte, MessageOffset)
	value[0] = MagicByte
	binary.BigEndian.PutUint32(value[1:MessageOffset], 7)

	req.NoError(handlePollEvent(&kafka.Message{Value: value}, "orders", active, eof, map[kafka.ErrorCode]bool{}))
	req.Equal(VALUEONLY, active[7])
	req.Empty(eof)
}

func TestHandlePollEvent_PartitionEOF(t *testing.T) {
	req := require.New(t)
	active := make(map[int32]int)
	eof := make(map[int32]bool)

	req.NoError(handlePollEvent(kafka.PartitionEOF{Partition: 2}, "orders", active, eof, map[kafka.ErrorCode]bool{}))
	req.True(eof[2])
	req.Empty(active)
}

func TestHandlePollEvent_FatalErrorReturned(t *testing.T) {
	req := require.New(t)
	err := handlePollEvent(kafka.NewError(kafka.ErrAllBrokersDown, "down", true),
		"orders", make(map[int32]int), make(map[int32]bool), map[kafka.ErrorCode]bool{})
	req.Error(err)
}

func TestHandlePollEvent_NonFatalErrorLoggedOncePerCode(t *testing.T) {
	req := require.New(t)
	eof := make(map[int32]bool)
	logged := make(map[kafka.ErrorCode]bool)
	e := kafka.NewError(kafka.ErrOffsetOutOfRange, "transient", false)

	req.NoError(handlePollEvent(e, "orders", make(map[int32]int), eof, logged))
	req.Empty(eof) // a non-fatal error must not be mistaken for EOF
	req.True(logged[kafka.ErrOffsetOutOfRange])

	// A repeat of the same code is deduped and still returns no error.
	req.NoError(handlePollEvent(e, "orders", make(map[int32]int), eof, logged))
}

func TestHandlePollEvent_NilTimeout(t *testing.T) {
	req := require.New(t)
	active := make(map[int32]int)
	eof := make(map[int32]bool)
	req.NoError(handlePollEvent(nil, "orders", active, eof, map[kafka.ErrorCode]bool{}))
	req.Empty(active)
	req.Empty(eof)
}

func TestExtractSchemaIDFromPayload_Valid(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	// Build magic byte + 4-byte schema ID (100002)
	payload := make([]byte, 10)
	payload[0] = 0x00 // magic byte
	binary.BigEndian.PutUint32(payload[1:5], 100002)

	extractSchemaIDFromPayload(payload, VALUEONLY, activeSchemas)
	req.Equal(VALUEONLY, activeSchemas[100002])
}

func TestExtractSchemaIDFromPayload_NoMagicByte(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	payload := []byte{0x01, 0x00, 0x00, 0x01, 0x02}
	extractSchemaIDFromPayload(payload, VALUEONLY, activeSchemas)
	req.Empty(activeSchemas)
}

func TestExtractSchemaIDFromPayload_TooShort(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	extractSchemaIDFromPayload([]byte{0x00, 0x01}, VALUEONLY, activeSchemas)
	req.Empty(activeSchemas)
}

func TestExtractSchemaIDFromPayload_Nil(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	extractSchemaIDFromPayload(nil, VALUEONLY, activeSchemas)
	req.Empty(activeSchemas)
}

func TestExtractSchemaIDsFromHeaders_ValueSchemaID_Binary(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	// 4-byte big-endian schema ID = 42
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 42)

	headers := []kafka.Header{
		{Key: "value.schema.id", Value: idBytes},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(VALUEONLY, activeSchemas[42])
}

func TestExtractSchemaIDsFromHeaders_KeySchemaID_Binary(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 99)

	headers := []kafka.Header{
		{Key: "key.schema.id", Value: idBytes},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(KEYONLY, activeSchemas[99])
}

func TestExtractSchemaIDsFromHeaders_StringEncoded(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	headers := []kafka.Header{
		{Key: "value.schema.id", Value: []byte("12345")},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(VALUEONLY, activeSchemas[12345])
}

func TestExtractSchemaIDsFromHeaders_SRPrefix(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 55)

	headers := []kafka.Header{
		{Key: "sr.value.schema.id", Value: idBytes},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(VALUEONLY, activeSchemas[55])
}

func TestExtractSchemaIDsFromHeaders_GenericSchemaID(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	// "schema.id" without key/value prefix is treated as value
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 77)

	headers := []kafka.Header{
		{Key: "schema.id", Value: idBytes},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(VALUEONLY, activeSchemas[77])
}

func TestExtractSchemaIDsFromHeaders_KeyAndValue(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	keyIDBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(keyIDBytes, 10)
	valueIDBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(valueIDBytes, 20)

	headers := []kafka.Header{
		{Key: "key.schema.id", Value: keyIDBytes},
		{Key: "value.schema.id", Value: valueIDBytes},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Equal(KEYONLY, activeSchemas[10])
	req.Equal(VALUEONLY, activeSchemas[20])
}

func TestExtractSchemaIDsFromHeaders_IgnoresUnrelatedHeaders(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	headers := []kafka.Header{
		{Key: "content-type", Value: []byte("application/json")},
		{Key: "correlation-id", Value: []byte("abc-123")},
		{Key: "x-custom-header", Value: []byte("test")},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Empty(activeSchemas)
}

func TestExtractSchemaIDsFromHeaders_EmptyValue(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	headers := []kafka.Header{
		{Key: "value.schema.id", Value: nil},
		{Key: "key.schema.id", Value: []byte{}},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Empty(activeSchemas)
}

func TestExtractSchemaIDsFromHeaders_InvalidStringValue(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	headers := []kafka.Header{
		{Key: "value.schema.id", Value: []byte("not-a-number")},
	}

	extractSchemaIDsFromHeaders(headers, activeSchemas)
	req.Empty(activeSchemas)
}

func TestPayloadAndHeadersCombined(t *testing.T) {
	req := require.New(t)
	activeSchemas := make(map[int32]int)

	// Value has magic byte schema ID 100
	payload := make([]byte, 10)
	payload[0] = 0x00
	binary.BigEndian.PutUint32(payload[1:5], 100)
	extractSchemaIDFromPayload(payload, VALUEONLY, activeSchemas)

	// Headers have a different schema ID 200
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 200)
	headers := []kafka.Header{{Key: "value.schema.id", Value: idBytes}}
	extractSchemaIDsFromHeaders(headers, activeSchemas)

	// Both should be detected
	req.Equal(VALUEONLY, activeSchemas[100])
	req.Equal(VALUEONLY, activeSchemas[200])
}

func TestClassifySchemaIDHeader(t *testing.T) {
	req := require.New(t)
	req.Equal(VALUEONLY, classifySchemaIDHeader("value.schema.id"))
	req.Equal(VALUEONLY, classifySchemaIDHeader("sr.value.schema.id"))
	req.Equal(VALUEONLY, classifySchemaIDHeader("schema.id"))
	req.Equal(KEYONLY, classifySchemaIDHeader("key.schema.id"))
	req.Equal(KEYONLY, classifySchemaIDHeader("sr.key.schema.id"))
	req.Equal(0, classifySchemaIDHeader("content-type"))
	req.Equal(0, classifySchemaIDHeader("x-schema-id"))
	req.Equal(0, classifySchemaIDHeader(""))
}

func TestParseSchemaIDFromHeaderValue(t *testing.T) {
	req := require.New(t)

	// 4-byte big-endian
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 42)
	req.Equal(int32(42), parseSchemaIDFromHeaderValue(idBytes))

	// String-encoded
	req.Equal(int32(12345), parseSchemaIDFromHeaderValue([]byte("12345")))

	// Empty
	req.Equal(int32(0), parseSchemaIDFromHeaderValue(nil))
	req.Equal(int32(0), parseSchemaIDFromHeaderValue([]byte{}))

	// Invalid string
	req.Equal(int32(0), parseSchemaIDFromHeaderValue([]byte("abc")))

	// 8-byte value (not 4-byte, not a valid int string)
	req.Equal(int32(0), parseSchemaIDFromHeaderValue([]byte{0, 0, 0, 0, 0, 0, 0, 1}))
}
