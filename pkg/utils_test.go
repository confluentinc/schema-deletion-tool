package pkg

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifySubject(t *testing.T) {
	req := require.New(t)
	req.True(IsKeySchema("test-key"))
	req.False(IsKeySchema("test"))
	req.True(VerifySubject("test-key"))
	req.False(IsValueSchema("test"))
	req.True(VerifySubject("test-key"))
	req.True(VerifySubject("test-value"))
	req.False(VerifySubject("test"))
}

func TestExtractTopicFromSubject(t *testing.T) {
	req := require.New(t)
	subjects := []string{
		"test",
		"test-key",
		"test-value",
		":.:test",
		":.mycontext:test",
		":.mycontext:test-key",
		":.mycontext:test-value",
	}
	topics := []string{
		"test",
		"test",
		"test",
		"test",
		"test",
		"test",
		"test",
	}
	req.Equal(topics, ExtractTopicFromSubject(subjects))
}

func TestIsValidChoice(t *testing.T) {
	req := require.New(t)
	validCandidates := []string{
		"Y", "N", "y", "n", "Yes", "No", "yes", "no", "YES", "NO",
	}
	for _, c := range validCandidates {
		req.True(IsValidChoice(c))
	}
	invalidCandidates := []string{
		"Ye", "nO", "yES", "abc",
	}
	for _, c := range invalidCandidates {
		req.False(IsValidChoice(c))
	}
}

func TestPrintTable(t *testing.T) {
	req := require.New(t)
	topicsWithClusterInfo := []TopicWithClusterInfo{
		{
			"topic-1",
			"lkc-123",
		},
		{
			"topic-2",
			"lkc-456",
		},
	}
	old := os.Stdout // keep backup of the real stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintTable(TopicInfoFields, topicsWithClusterInfo, false)
	_ = w.Close()
	out, _ := ioutil.ReadAll(r)
	req.Equal("+---------+-----------+\n"+
		"| TOPIC   | CLUSTERID |\n"+
		"+---------+-----------+\n"+
		"| topic-1 | lkc-123   |\n"+
		"| topic-2 | lkc-456   |\n"+
		"+---------+-----------+\n", string(out))

	r, w, _ = os.Pipe()
	os.Stdout = w

	PrintTable(TopicInfoFields, topicsWithClusterInfo, true)
	_ = w.Close()
	out, _ = ioutil.ReadAll(r)
	req.Equal("+---+---------+-----------+\n"+
		"|   | TOPIC   | CLUSTERID |\n"+
		"+---+---------+-----------+\n"+
		"| 0 | topic-1 | lkc-123   |\n"+
		"| 1 | topic-2 | lkc-456   |\n"+
		"+---+---------+-----------+\n", string(out))
	os.Stdout = old
}
