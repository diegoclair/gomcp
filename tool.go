package gomcp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Wraps the error a tool's panic becomes; PostInvoke and the ErrorPolicy receive it, and the client reads a
// tool error while the server keeps serving.
var ErrToolPanicked = errors.New("gomcp: tool panicked")

type ToolDef interface {
	toolName() string
	register(server *mcp.Server, cfg *config)
}

type ToolOption func(*mcp.ToolAnnotations)

func ReadOnly() ToolOption {
	return func(a *mcp.ToolAnnotations) { a.ReadOnlyHint = true }
}

// Destructive is meaningful only on a tool that is not ReadOnly; MCP assumes
// true when it is never set.
func Destructive(destructive bool) ToolOption {
	return func(a *mcp.ToolAnnotations) { a.DestructiveHint = &destructive }
}

func Idempotent() ToolOption {
	return func(a *mcp.ToolAnnotations) { a.IdempotentHint = true }
}

func Title(title string) ToolOption {
	return func(a *mcp.ToolAnnotations) { a.Title = title }
}

// Every tool declares a closed world: it acts on the caller's own data only.
func Tool[In, Out any](name, description string, handler func(ctx context.Context, in In) (Out, error), opts ...ToolOption) ToolDef {
	openWorld := false
	annotations := &mcp.ToolAnnotations{OpenWorldHint: &openWorld}
	for _, opt := range opts {
		opt(annotations)
	}
	return &typedTool[In, Out]{
		tool:    &mcp.Tool{Name: name, Description: description, Annotations: annotations},
		handler: handler,
	}
}

type typedTool[In, Out any] struct {
	tool    *mcp.Tool
	handler func(ctx context.Context, in In) (Out, error)
}

func (t *typedTool[In, Out]) toolName() string { return t.tool.Name }

func (t *typedTool[In, Out]) register(server *mcp.Server, cfg *config) {
	mcp.AddTool(server, t.toolWithInputSchema(cfg), func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		call := Call{Tool: t.tool.Name, Input: in}
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			call.Caller = req.Extra.TokenInfo
			ctx = withCaller(ctx, req.Extra.TokenInfo)
		}
		out, err := t.invoke(ctx, call, in, cfg)
		if err != nil {
			var zero Out
			return nil, zero, errors.New(cfg.errorPolicy(err))
		}
		return nil, out, nil
	})
}

func (t *typedTool[In, Out]) invoke(ctx context.Context, call Call, in In, cfg *config) (out Out, err error) {
	if cfg.postInvoke != nil {
		defer func() { cfg.postInvoke(ctx, call, out, err) }()
	}
	// The SDK runs a tool where nothing recovers, so a panic would take the whole process down.
	defer func() {
		if recovered := recover(); recovered != nil {
			var zero Out
			out, err = zero, fmt.Errorf("%w: tool %s: %v\n%s", ErrToolPanicked, call.Tool, recovered, debug.Stack())
		}
	}()
	if cfg.preInvoke != nil {
		scoped, rejected := cfg.preInvoke(ctx, call)
		if rejected != nil {
			return out, rejected
		}
		ctx = scoped
	}
	return t.handler(ctx, in)
}

// Without type schemas the SDK infers the input schema itself.
func (t *typedTool[In, Out]) toolWithInputSchema(cfg *config) *mcp.Tool {
	if len(cfg.typeSchemas) == 0 {
		return t.tool
	}
	inputType := reflect.TypeFor[In]()
	if inputType == reflect.TypeFor[any]() {
		return t.tool
	}
	if inputType.Kind() == reflect.Pointer {
		inputType = inputType.Elem()
	}
	schema, err := jsonschema.ForType(inputType, &jsonschema.ForOptions{TypeSchemas: cfg.typeSchemas})
	if err != nil {
		panic(fmt.Sprintf("gomcp: tool %q: input schema: %v", t.tool.Name, err))
	}
	tool := *t.tool
	tool.InputSchema = schema
	return &tool
}
