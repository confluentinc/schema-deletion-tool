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
