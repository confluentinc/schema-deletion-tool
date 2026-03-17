package pkg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseContext_WithContext(t *testing.T) {
	req := require.New(t)
	ctx, raw := ParseContext(":.mycontext:orders-value")
	req.Equal("mycontext", ctx)
	req.Equal("orders-value", raw)
}

func TestParseContext_DefaultContext(t *testing.T) {
	req := require.New(t)
	ctx, raw := ParseContext(":.:orders-value")
	req.Equal("", ctx)
	req.Equal("orders-value", raw)
}

func TestParseContext_NoContext(t *testing.T) {
	req := require.New(t)
	ctx, raw := ParseContext("orders-value")
	req.Equal("", ctx)
	req.Equal("orders-value", raw)
}

func TestParseContext_MalformedPrefix(t *testing.T) {
	req := require.New(t)
	// Has prefix but no closing colon
	ctx, raw := ParseContext(":.mycontext")
	req.Equal("", ctx)
	req.Equal(":.mycontext", raw)
}

func TestBuildContextSubject(t *testing.T) {
	req := require.New(t)
	req.Equal(":.mycontext:orders-value", BuildContextSubject("mycontext", "orders-value"))
	req.Equal("orders-value", BuildContextSubject("", "orders-value"))
}

func TestHasContext(t *testing.T) {
	req := require.New(t)
	req.True(HasContext(":.mycontext:orders-value"))
	req.True(HasContext(":.:orders-value"))
	req.False(HasContext("orders-value"))
}

func TestGetRawSubject(t *testing.T) {
	req := require.New(t)
	req.Equal("orders-value", GetRawSubject(":.mycontext:orders-value"))
	req.Equal("orders-value", GetRawSubject("orders-value"))
}

func TestGroupByContext(t *testing.T) {
	req := require.New(t)
	subjects := []string{
		"orders-value",
		":.staging:orders-value",
		":.staging:payments-value",
		":.production:orders-value",
	}
	groups := GroupByContext(subjects)
	req.Len(groups, 3) // "", "staging", "production"
	req.Len(groups[""], 1)
	req.Len(groups["staging"], 2)
	req.Len(groups["production"], 1)
}
