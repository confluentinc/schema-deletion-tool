package pkg

import (
	"fmt"
	"sort"
	"strings"
)

// VerifySubjectForStrategy checks if a subject matches the given naming strategy.
func VerifySubjectForStrategy(subject string, strategy string) bool {
	// Strip context prefix before checking strategy
	raw := GetRawSubject(subject)

	switch strategy {
	case "topic-name":
		return IsValueSchema(raw) || IsKeySchema(raw)
	case "record-name":
		return len(raw) > 0
	case "topic-record-name":
		// Must have a hyphen followed by a segment containing a dot (qualified record name).
		// e.g., "orders-com.example.Order" — hyphen at index 6, dot after it.
		hyphenIdx := strings.Index(raw, "-")
		if hyphenIdx <= 0 {
			return false
		}
		afterHyphen := raw[hyphenIdx+1:]
		return strings.Contains(afterHyphen, ".")
	default:
		return IsValueSchema(raw) || IsKeySchema(raw)
	}
}

// SubjectsForFailedTopics returns the subset of allSubjects that resolve to one
// of failedTopics under the given naming strategy. These subjects could not have
// their usage verified and must be protected from deletion.
//
// For record-name, subjects are not tied to topics by name and active schema IDs
// are tracked globally, so any unscannable topic leaves every subject unverifiable.
func SubjectsForFailedTopics(failedTopics, allSubjects, allTopics []string, strategy string) map[string]bool {
	unverified := make(map[string]bool)
	if len(failedTopics) == 0 {
		return unverified
	}

	failedSet := make(map[string]bool, len(failedTopics))
	for _, t := range failedTopics {
		failedSet[t] = true
	}

	switch strategy {
	case "record-name":
		for _, s := range allSubjects {
			unverified[s] = true
		}
	case "topic-record-name":
		sortedTopics := topicsByDescendingLength(allTopics)
		for _, s := range allSubjects {
			if t, ok := longestPrefixTopic(GetRawSubject(s), sortedTopics); ok && failedSet[t] {
				unverified[s] = true
			}
		}
	default: // topic-name
		for _, s := range allSubjects {
			if failedSet[TopicFromSubject(s)] {
				unverified[s] = true
			}
		}
	}
	return unverified
}

// topicsByDescendingLength returns a copy of topics sorted longest-first, so a
// prefix scan finds the most specific match first (TopicRecordNameStrategy).
func topicsByDescendingLength(topics []string) []string {
	sorted := make([]string, len(topics))
	copy(sorted, topics)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	return sorted
}

// longestPrefixTopic returns the longest topic in sortedTopics (which must be
// sorted longest-first) that is a prefix of raw followed by '-', matching how
// TopicRecordNameStrategy names subjects as "<topic>-<record>".
func longestPrefixTopic(raw string, sortedTopics []string) (string, bool) {
	for _, t := range sortedTopics {
		if strings.HasPrefix(raw, t+"-") {
			return t, true
		}
	}
	return "", false
}

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
		return nil, fmt.Errorf("--topics or --all-topics is required with record-name strategy")
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

	topicNames := make([]string, 0, len(allTopics))
	for t := range allTopics {
		topicNames = append(topicNames, t)
	}
	sortedTopics := topicsByDescendingLength(topicNames)

	// Match subjects to topics
	matchedTopics := make(map[string]bool)
	for _, subject := range subjects {
		if topic, ok := longestPrefixTopic(GetRawSubject(subject), sortedTopics); ok {
			matchedTopics[topic] = true
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
