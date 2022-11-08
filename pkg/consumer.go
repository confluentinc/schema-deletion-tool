package pkg

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
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
	if err := ccfg.SetKey("group.id", "console-schema-deletion-tool"); err != nil {
		return nil, err
	}
	return ccfg, nil
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
		low, high, err := consumer.QueryWatermarkOffsets(topic, i, -1)
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

	for !checkIfReachesOffsets(endOffsets, offsets) {
		msg, err := consumer.ReadMessage(5 * time.Second)
		if err != nil {
			return nil, err
		}
		offsets[msg.TopicPartition.Partition] = msg.TopicPartition.Offset

		value := msg.Value
		if len(value) >= MessageOffset && value[0] == MagicByte {
			schemaID := int32(binary.BigEndian.Uint32(value[1:MessageOffset]))
			activeSchemas[schemaID] = activeSchemas[schemaID] | VALUEONLY
		}
		key := msg.Key
		if len(key) >= MessageOffset && key[0] == MagicByte {
			schemaID := int32(binary.BigEndian.Uint32(key[1:MessageOffset]))
			activeSchemas[schemaID] = activeSchemas[schemaID] | KEYONLY
		}
	}

	return activeSchemas, nil
}

func checkIfReachesOffsets(endOffsets, offsets map[int32]kafka.Offset) bool {
	for i := int32(0); i < 6; i++ {
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
