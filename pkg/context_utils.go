package pkg

import (
	"strings"
)

// ParseContext extracts the context and raw subject from a context-qualified subject.
// Input: ":.mycontext:orders-value" -> context="mycontext", subject="orders-value"
// Input: "orders-value" -> context="", subject="orders-value"
func ParseContext(subject string) (context string, rawSubject string) {
	if !strings.HasPrefix(subject, CONTEXT_PREFIX) {
		return "", subject
	}
	rest := subject[len(CONTEXT_PREFIX):]
	idx := strings.Index(rest, CONTEXT_SUFFIX)
	if idx == -1 {
		return "", subject
	}
	return rest[:idx], rest[idx+1:]
}

// BuildContextSubject creates a context-qualified subject name.
// context="mycontext", subject="orders-value" -> ":.mycontext:orders-value"
// context="", subject="orders-value" -> "orders-value"
func BuildContextSubject(context string, subject string) string {
	if context == "" {
		return subject
	}
	return CONTEXT_PREFIX + context + CONTEXT_SUFFIX + subject
}

// HasContext returns true if the subject has a context prefix.
func HasContext(subject string) bool {
	return strings.HasPrefix(subject, CONTEXT_PREFIX)
}

// GetContextFromSubject returns the context portion of a subject, or empty string.
func GetContextFromSubject(subject string) string {
	ctx, _ := ParseContext(subject)
	return ctx
}

// GetRawSubject returns the subject without context prefix.
func GetRawSubject(subject string) string {
	_, raw := ParseContext(subject)
	return raw
}

// GroupByContext groups subjects by their context.
func GroupByContext(subjects []string) map[string][]string {
	groups := make(map[string][]string)
	for _, s := range subjects {
		ctx := GetContextFromSubject(s)
		groups[ctx] = append(groups[ctx], s)
	}
	return groups
}
