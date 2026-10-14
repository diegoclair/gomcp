// Package gomcp builds a Model Context Protocol server from typed Go handlers
// and serves it over stateless Streamable HTTP behind a bearer token.
package gomcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caller is nil when the transport carried no bearer token.
type Call struct {
	Tool   string
	Input  any
	Caller *auth.TokenInfo
}

// The context returned is the one the handler receives; an error rejects the
// call before the handler runs.
type PreInvoke func(ctx context.Context, call Call) (context.Context, error)

// Receives the handler's own error, before the ErrorPolicy rewrites it.
type PostInvoke func(ctx context.Context, call Call, output any, err error)

type ErrorPolicy func(err error) string

type Option func(*config)

type config struct {
	preInvoke   PreInvoke
	postInvoke  PostInvoke
	errorPolicy ErrorPolicy
}

func WithPreInvoke(hook PreInvoke) Option {
	return func(c *config) { c.preInvoke = hook }
}

func WithPostInvoke(hook PostInvoke) Option {
	return func(c *config) { c.postInvoke = hook }
}

// The default shows the client err.Error().
func WithErrorPolicy(policy ErrorPolicy) Option {
	return func(c *config) { c.errorPolicy = policy }
}

func showErrorMessage(err error) string { return err.Error() }

// NewServer panics on a duplicate tool name or a type whose schema cannot be
// inferred, both of which are wiring mistakes caught at boot.
func NewServer(name, version string, tools []ToolDef, opts ...Option) *mcp.Server {
	cfg := &config{errorPolicy: showErrorMessage}
	for _, opt := range opts {
		opt(cfg)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil)
	registered := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolName := tool.toolName()
		if registered[toolName] {
			panic(fmt.Sprintf("gomcp: tool %q registered twice", toolName))
		}
		registered[toolName] = true
		tool.register(server, cfg)
	}
	return server
}
