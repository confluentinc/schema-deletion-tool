package pkg

import (
	"testing"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/stretchr/testify/require"
)

func TestCreateConsumer(t *testing.T) {
	req := require.New(t)
	consumer, err := CreateConsumer("", Credentials{"key", "value"})
	req.NoError(err)
	req.NotNil(consumer)

	topic := "my-topic"
	err = consumer.Assign([]kafka.TopicPartition{
		{
			Topic:     &topic,
			Partition: 0,
			Offset:    kafka.OffsetBeginning,
		},
		{
			Topic:     &topic,
			Partition: 1,
			Offset:    kafka.OffsetBeginning,
		},
		{
			Topic:     &topic,
			Partition: 2,
			Offset:    kafka.OffsetBeginning,
		},
	})
	req.NoError(err)

	assignment, err := consumer.Assignment()
	req.NoError(err)
	req.Equal(3, len(assignment))
	for i := 0; i < 3; i++ {
		req.Equal(topic, *assignment[i].Topic)
		req.Equal(int32(i), assignment[i].Partition)
	}
}

func TestCompareOffsets(t *testing.T) {
	req := require.New(t)
	req.True(checkIfReachesOffsets(map[int32]kafka.Offset{0: kafka.OffsetEnd}, map[int32]kafka.Offset{}, 1))
	req.True(checkIfReachesOffsets(map[int32]kafka.Offset{0: kafka.OffsetEnd}, map[int32]kafka.Offset{0: 0}, 1))
	req.False(checkIfReachesOffsets(map[int32]kafka.Offset{0: 0}, map[int32]kafka.Offset{}, 1))
	req.True(checkIfReachesOffsets(map[int32]kafka.Offset{0: 2}, map[int32]kafka.Offset{0: 2}, 1))
	req.False(checkIfReachesOffsets(map[int32]kafka.Offset{0: 2}, map[int32]kafka.Offset{0: 1}, 1))
	req.False(checkIfReachesOffsets(map[int32]kafka.Offset{0: 2, 1: 4}, map[int32]kafka.Offset{0: 1, 1: 4}, 2))
	req.False(checkIfReachesOffsets(map[int32]kafka.Offset{0: 2, 1: 4}, map[int32]kafka.Offset{0: 1, 1: 4}, 2))
}
