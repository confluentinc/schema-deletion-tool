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
	ctx, raw := ParseContext(":.mycontext")
	req.Equal("", ctx)
	req.Equal(":.mycontext", raw)
}

func TestGetRawSubject(t *testing.T) {
	req := require.New(t)
	req.Equal("orders-value", GetRawSubject(":.mycontext:orders-value"))
	req.Equal("orders-value", GetRawSubject("orders-value"))
}

func TestGetContextFromSubject(t *testing.T) {
	req := require.New(t)
	req.Equal("mycontext", GetContextFromSubject(":.mycontext:orders-value"))
	req.Equal("", GetContextFromSubject("orders-value"))
	req.Equal("", GetContextFromSubject(":.:orders-value"))
}
