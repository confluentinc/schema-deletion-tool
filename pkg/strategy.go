package pkg

import (
	"fmt"
	"sort"
	"strings"
)

// ResolveTopics determines which topics to scan based on the naming strategy.
func ResolveTopics(subjects []string, strategy string, explicitTopics []string, scanAllTopics bool, platform Platform, clusters []string) ([]TopicWithClusterInfo, error) {
	if len(explicitTopics) > 0 {
		return resolveExplicitTopics(explicitTopics, platform, clusters)
	}

	if scanAllTopics {
		return resolveAllTopics(platform, clusters)
	}

	switch strategy {
	case "topic-name":
		topics := ExtractTopicFromSubject(subjects)
		return matchTopicsToClusters(topics, platform, clusters)
	case "topic-record-name":
		return resolveTopicRecordName(subjects, platform, clusters)
	case "record-name":
		return nil, fmt.Errorf("--topics or --scan-all-topics is required with record-name strategy")
	default:
		topics := ExtractTopicFromSubject(subjects)
		return matchTopicsToClusters(topics, platform, clusters)
	}
}

func resolveExplicitTopics(explicitTopics []string, platform Platform, clusters []string) ([]TopicWithClusterInfo, error) {
	var result []TopicWithClusterInfo
	for _, clusterID := range clusters {
		clusterTopics, err := platform.ListTopics(clusterID)
		if err != nil {
			return nil, err
		}
		topicSet := make(map[string]bool)
		for _, t := range clusterTopics {
			topicSet[t] = true
		}
		for _, t := range explicitTopics {
			if topicSet[t] {
				result = append(result, TopicWithClusterInfo{Topic: t, ClusterID: clusterID})
			}
		}
	}
	return result, nil
}

func resolveAllTopics(platform Platform, clusters []string) ([]TopicWithClusterInfo, error) {
	var result []TopicWithClusterInfo
	for _, clusterID := range clusters {
		topics, err := platform.ListTopics(clusterID)
		if err != nil {
			return nil, err
		}
		for _, t := range topics {
			result = append(result, TopicWithClusterInfo{Topic: t, ClusterID: clusterID})
		}
	}
	fmt.Printf("Found %d topic(s) across %d cluster(s) for scanning.\n", len(result), len(clusters))
	return result, nil
}

func matchTopicsToClusters(topics []string, platform Platform, clusters []string) ([]TopicWithClusterInfo, error) {
	var result []TopicWithClusterInfo
	for _, clusterID := range clusters {
		clusterTopics, err := platform.ListTopics(clusterID)
		if err != nil {
			return nil, err
		}
		for _, ct := range clusterTopics {
			if ContainsTopic(ct, topics) {
				result = append(result, TopicWithClusterInfo{Topic: ct, ClusterID: clusterID})
			}
		}
	}
	fmt.Printf("Found %d topic(s).\n", len(result))
	return result, nil
}

// resolveTopicRecordName uses longest-prefix matching to find topics for
// TopicRecordNameStrategy subjects.
func resolveTopicRecordName(subjects []string, platform Platform, clusters []string) ([]TopicWithClusterInfo, error) {
	// Gather all known topics
	allTopics := make(map[string][]string) // topic -> clusterIDs
	for _, clusterID := range clusters {
		topics, err := platform.ListTopics(clusterID)
		if err != nil {
			return nil, err
		}
		for _, t := range topics {
			allTopics[t] = append(allTopics[t], clusterID)
		}
	}

	// Sort topics by length descending for longest-prefix matching
	sortedTopics := make([]string, 0, len(allTopics))
	for t := range allTopics {
		sortedTopics = append(sortedTopics, t)
	}
	sort.Slice(sortedTopics, func(i, j int) bool {
		return len(sortedTopics[i]) > len(sortedTopics[j])
	})

	// Match subjects to topics
	matchedTopics := make(map[string]bool)
	for _, subject := range subjects {
		// Strip context prefix if present
		rawSubject := subject
		if strings.HasPrefix(rawSubject, CONTEXT_PREFIX) {
			rawSubject = rawSubject[len(CONTEXT_PREFIX):]
			idx := strings.Index(rawSubject, CONTEXT_SUFFIX)
			if idx != -1 {
				rawSubject = rawSubject[idx+1:]
			}
		}

		for _, topic := range sortedTopics {
			if strings.HasPrefix(rawSubject, topic+"-") {
				matchedTopics[topic] = true
				break // Longest match found
			}
		}
	}

	var result []TopicWithClusterInfo
	for topic := range matchedTopics {
		for _, clusterID := range allTopics[topic] {
			result = append(result, TopicWithClusterInfo{Topic: topic, ClusterID: clusterID})
		}
	}

	fmt.Printf("Resolved %d topic(s) from TopicRecordNameStrategy subjects.\n", len(result))
	return result, nil
}
