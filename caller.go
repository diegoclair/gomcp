package gomcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

type callerKey struct{}

func withCaller(ctx context.Context, caller *auth.TokenInfo) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// Nil outside a tool call that carried a verified bearer token.
func Caller(ctx context.Context) *auth.TokenInfo {
	caller, _ := ctx.Value(callerKey{}).(*auth.TokenInfo)
	return caller
}

// False when there is no token or its UserID is empty, so a missing identity
// is never read as a user.
func UserID(ctx context.Context) (string, bool) {
	caller := Caller(ctx)
	if caller == nil || caller.UserID == "" {
		return "", false
	}
	return caller.UserID, true
}
