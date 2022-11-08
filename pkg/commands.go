package pkg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func GetAllEligibleSubjects() ([]string, error) {
	var subjects []string
	output, err := ExecuteCommand(Confluent, []string{"schema-registry", "subject", "list", "-o", "json"}, false)
	if err != nil {
		return nil, err
	}
	var _subjects []map[string]string
	err = json.Unmarshal(output, &_subjects)
	if err != nil {
		return nil, err
	}
	for _, s := range _subjects {
		if VerifySubject(s["subject"]) {
			subjects = append(subjects, s["subject"])
		}
	}
	return subjects, nil
}

func ListClusters(ctx *Context) error {
	fmt.Println("Listing all clusters under the environment...")
	output, err := ExecuteCommand(Confluent, []string{"kafka", "cluster", "list", "-o", "json"}, false)
	if err != nil {
		return err
	}
	var clusters []KafkaCluster
	err = json.Unmarshal(output, &clusters)
	if err != nil {
		return err
	}

	PrintTable(KafkaClusterFields, clusters, false)

	fmt.Print("Please select the clusters you want to skip, with cluster IDs separated by comma: ")
	resp, err := ReadLine()
	if err != nil {
		return err
	}
	skipped := make(map[string]struct{})
	for _, lkcId := range strings.Split(resp, ",") {
		skipped[lkcId] = struct{}{}
	}

	var clusterCandidates []string
	for _, kafkaCluster := range clusters {
		if _, ok := skipped[kafkaCluster.ID]; !ok {
			clusterCandidates = append(clusterCandidates, kafkaCluster.ID)
		}
	}

	return ctx.SetClusters(clusterCandidates)
}

func ListAndScanTopics(ctx *Context) (map[int32]int, error) {
	topicsWithClusterInfo, err := listTopics(ctx)
	if err != nil {
		return nil, err
	}
	usedSchemas, err := scanTopics(topicsWithClusterInfo, ctx)
	if err != nil {
		return nil, err
	}

	return usedSchemas, nil
}

func GetAllSchemas(ctx *Context) ([]SchemaInfo, error) {
	var schemas []SchemaInfo
	for _, subject := range ctx.Subjects {
		var _schemas []SchemaInfo
		fmt.Printf("Scanning schemas under subject %s...", subject)
		output, err := ExecuteCommand(Confluent, []string{"schema-registry", "schema", "list", "--subject-prefix", subject, "-o", "json"}, false)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(output, &_schemas); err != nil {
			return nil, err
		}

		fmt.Printf("  Found %d schema(s).\n", len(_schemas))
		schemas = append(schemas, _schemas...)
	}

	return schemas, nil
}

func listTopics(ctx *Context) ([]TopicWithClusterInfo, error) {
	fmt.Print("Scanning clusters for eligible topics...")
	var topicsWithClusterInfo []TopicWithClusterInfo
	for _, cluster := range ctx.Clusters {
		output, err := ExecuteCommand(Confluent, []string{"kafka", "topic", "list", "--cluster", cluster, "-o", "json"}, false)
		if err != nil {
			return nil, err
		}
		var topics []TopicName
		if err = json.Unmarshal(output, &topics); err != nil {
			return nil, err
		}
		for _, topic := range topics {
			if ContainsTopic(topic.Name, ctx.Topics) {
				topicsWithClusterInfo = append(topicsWithClusterInfo, TopicWithClusterInfo{topic.Name, cluster})
			}
		}
	}

	fmt.Printf("Found %d topic(s).\n", len(topicsWithClusterInfo))
	PrintTable(TopicInfoFields, topicsWithClusterInfo, false)

	return topicsWithClusterInfo, nil
}

func scanTopics(topics []TopicWithClusterInfo, ctx *Context) (map[int32]int, error) {
	activeSchemas := make(map[int32]int)
	for _, topic := range topics {
		fmt.Printf("Scanning topic %s%s%s from cluster %s...\n", GREEN, topic.Topic, RESET, topic.ClusterID)
		output, err := ExecuteCommand(Confluent, []string{"kafka", "cluster", "describe", topic.ClusterID, "-o", "json"}, false)
		var cluster map[string]interface{}
		if err = json.Unmarshal(output, &cluster); err != nil {
			return nil, err
		}
		consumer, err := CreateConsumer(cluster["endpoint"].(string), ctx.Credentials[topic.ClusterID])
		if err != nil {
			return nil, err
		}

		_activeSchemas, err := scanActiveSchemas(consumer, topic.Topic)
		if err != nil {
			return nil, err
		}
		for k, v := range _activeSchemas {
			activeSchemas[k] = activeSchemas[k] | v
		}
	}
	return activeSchemas, nil
}

func SelectDeletionCandidates(schemas []SchemaInfo, usedSchemas map[int32]int) ([]SchemaInfo, error) {
	var candidates []SchemaInfo
	for _, schema := range schemas {
		schemaId, _ := strconv.ParseInt(schema.SchemaID, 10, 32)
		if IsKeySchema(schema.Subject) {
			if usedSchemas[int32(schemaId)]&KEYONLY == 0 {
				candidates = append(candidates, schema)
			}
		} else {
			if usedSchemas[int32(schemaId)]&VALUEONLY == 0 {
				candidates = append(candidates, schema)
			}
		}
	}
	fmt.Printf("Following %d schemas are unused schemas that qualify for deletion.\n", len(candidates))
	PrintTable(SchemaInfoFields, candidates, true)

	var selection []SchemaInfo
	for {
		fmt.Print("Please select the schemas you want to delete by typing the numbers (1st column), separated by comma: ")
		resp, err := ReadLine()
		if err != nil {
			return nil, err
		}
		if resp == "all" {
			selection = candidates
		} else if len(resp) > 0 {
			numbers := strings.Split(resp, ",")

			selection = make([]SchemaInfo, 0)
			for _, n := range numbers {
				no, err := strconv.ParseInt(n, 10, 0)
				if err != nil {
					return nil, err
				}
				selection = append(selection, candidates[no])
			}
		}
		PrintTable(SchemaInfoFields, selection, true)
		for {
			fmt.Printf("Confirm deletion of above schemas by typing Y/N: %s", RED)
			resp, err = ReadLine()
			ResetColor()
			if err != nil {
				return nil, err
			}
			if IsValidChoice(resp) {
				break
			}
		}
		if IsYes(resp) {
			break
		}
	}

	return selection, nil
}

func DeleteSchemas(schemas []SchemaInfo) error {
	var err error
	for _, schema := range schemas {
		_, err = ExecuteCommand(Confluent, []string{"schema-registry", "schema", "delete", "--subject", schema.Subject, "--version", schema.Version}, true)
		if err != nil {
			return err
		}
	}
	var resp string
	for {
		fmt.Printf("Confirm %shard%s deletion of above schemas by typing Y/N, hard deleted schemas cannot be recovered: %s", RED, RESET, RED)
		resp, err = ReadLine()
		ResetColor()
		if err != nil {
			return err
		}
		if IsValidChoice(resp) {
			break
		}
	}
	if IsYes(resp) {
		for _, schema := range schemas {
			_, err = ExecuteCommand(Confluent, []string{"schema-registry", "schema", "delete", "--subject", schema.Subject, "--version", schema.Version, "-P"}, true)
			if err != nil {
				return err
			}
		}
		fmt.Printf("Cleaned up a total of %d schemas.\n", len(schemas))
	}
	return nil
}
