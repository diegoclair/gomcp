// Package gomcp builds a Model Context Protocol server from typed Go handlers
// and serves it over stateless Streamable HTTP behind a bearer token.
package gomcp

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
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
	preInvoke         PreInvoke
	postInvoke        PostInvoke
	errorPolicy       ErrorPolicy
	instructions      string
	typeSchemas       map[reflect.Type]*jsonschema.Schema
	openOutputSchemas bool
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

// Sent once in the initialize result, for what no single tool's description can carry.
func WithInstructions(instructions string) Option {
	return func(c *config) { c.instructions = instructions }
}

// Lets a closed set of values the caller owns reach the client as an enum on every input field of that type, which
// the SDK then enforces before the handler runs.
func WithTypeSchemas(schemas map[reflect.Type]*jsonschema.Schema) Option {
	return func(c *config) { c.typeSchemas = schemas }
}

// Publishes every typed tool's output schema with no required field and no closed object, so a client that cached
// it before a deploy still accepts an output that gained or lost a field.
func WithOpenOutputSchemas() Option {
	return func(c *config) { c.openOutputSchemas = true }
}

// NewServer panics on a duplicate tool name or a type whose schema cannot be
// inferred, both of which are wiring mistakes caught at boot.
func NewServer(name, version string, tools []ToolDef, opts ...Option) *mcp.Server {
	cfg := &config{errorPolicy: showErrorMessage}
	for _, opt := range opts {
		opt(cfg)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version},
		&mcp.ServerOptions{Instructions: cfg.instructions})
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
