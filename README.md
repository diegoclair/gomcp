# gomcp

<p align="center">
 <b>A typed, opinionated MCP server layer for your Go backend</b><br>
    <a href="https://github.com/diegoclair/gomcp/tags" alt="GitHub tag">
     <img src="https://img.shields.io/github/tag/diegoclair/gomcp.svg" />
    </a>
    <a href='https://coveralls.io/github/diegoclair/gomcp?branch=main'>
     <img src='https://coveralls.io/repos/github/diegoclair/gomcp/badge.svg?branch=main' alt='Coverage Status' />
    </a>
    <a href="https://github.com/diegoclair/gomcp/actions">
     <img src="https://github.com/diegoclair/gomcp/actions/workflows/ci.yml/badge.svg" alt="build status">
    </a>
    <a href="https://github.com/diegoclair/gomcp/contributors" alt="Contributors">
     <img src="https://img.shields.io/github/contributors/diegoclair/gomcp" />
    </a>
    <a href="https://opensource.org/licenses/MIT">
     <img src="https://img.shields.io/badge/License-MIT-yellow.svg" />
    </a>
</p>

A Model Context Protocol server for a Go backend, built from typed handlers
and served over stateless Streamable HTTP behind a bearer token. A thin,
opinionated layer over the official
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk)
(v1.8.0): the SDK speaks the protocol, gomcp fixes the choices every backend
would otherwise make again — who the caller is, what a failed call shows, and
which requests never reach a tool.

The package imports no web framework: `Handler` returns an `http.Handler`
you mount on whatever router you already run.

## Install

```sh
go get github.com/diegoclair/gomcp
```

Requires **Go 1.27+**.

## Quickstart

```go
type GetCardIn struct {
    CardID int64 `json:"cardId" jsonschema:"the card id"`
}

type GetCardOut struct {
    Name       string `json:"name"`
    LimitCents int64  `json:"limitCents"`
}

getCard := gomcp.Tool("fin_get_card", "Returns one card. Amounts are in cents.",
    func(ctx context.Context, in GetCardIn) (GetCardOut, error) {
        return cards.Get(ctx, in.CardID)
    },
    gomcp.ReadOnly(), gomcp.Title("Get card"))

server := gomcp.NewServer("financial-control", version, []gomcp.ToolDef{getCard},
    gomcp.WithPreInvoke(scopeToUser),
    gomcp.WithPostInvoke(audit),
    gomcp.WithErrorPolicy(showOnlyBusinessErrors))

handler, err := gomcp.Handler(server, gomcp.HandlerOptions{
    Verifier: verifyToken,
})
```

The input schema is inferred from `In` (with `jsonschema:` struct tags) and the
output schema from `Out`; the SDK validates the arguments before your handler
runs, and the output becomes the result's `structuredContent`.

## Tools and their hints

| Option | Hint | Use it for |
|---|---|---|
| `ReadOnly()` | `readOnlyHint: true` | reads, previews |
| `Destructive(false)` | `destructiveHint: false` | creates that only add |
| `Destructive(true)` or nothing | `destructiveHint` unset, which MCP reads as true | updates that overwrite |
| `Idempotent()` | `idempotentHint: true` | a create keyed by an idempotency key |
| `Title(s)` | `title` | the name a client shows |

Every tool declares `openWorldHint: false`: a tool built with gomcp acts on
the caller's own data, never on an open world of external entities.

## Who is calling

`Handler` requires a `Verifier` (`auth.TokenVerifier`). The token it accepts
reaches every tool call three ways:

- `Call.Caller` in the `PreInvoke` and `PostInvoke` hooks;
- `gomcp.Caller(ctx)`, the full `*auth.TokenInfo`;
- `gomcp.UserID(ctx)`, which answers `false` when there is no identity, so an
  empty id is never read as a user.

`PreInvoke` returns the context the handler receives, which is the supported
way to scope a call to its user with your own context keys:

```go
func scopeToUser(ctx context.Context, call gomcp.Call) (context.Context, error) {
    if call.Caller == nil {
        return nil, errors.New("unauthenticated")
    }
    return reqctx.WithUserID(ctx, call.Caller.UserID), nil
}
```

A `PreInvoke` error rejects the call before the handler runs.

The verifier returns an error wrapping `auth.ErrInvalidToken` for an unknown
or revoked token, which answers 401; any other error answers 500. A
`TokenInfo` without an `Expiration` is accepted, because a token that never
expires is valid for as long as the verifier says so:

```go
func verifyToken(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
    userID, err := tokens.Resolve(ctx, token)
    if errors.Is(err, ErrTokenNotFound) {
        return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
    }
    if err != nil {
        return nil, err
    }
    return &auth.TokenInfo{UserID: userID}, nil
}
```

## Server instructions

`WithInstructions` sets the text a client receives once, in the initialize
result: the vocabulary and conventions every tool shares, which no single
tool's description should repeat.

## Closed sets of values

`WithTypeSchemas` maps a Go type to the schema every tool input field of that
type gets, instead of the inferred one. Give the field a named type and build
its enum from the code that owns the values; the SDK then refuses any other
value before the handler runs.

```go
type marketplace string

gomcp.NewServer("shop", "v1", tools, gomcp.WithTypeSchemas(map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[marketplace](): {Type: "string", Enum: []any{"north", "south"}},
}))
```

## Output schemas that survive a deploy

A client caches each tool's output schema when it connects and validates the
result's `structuredContent` against it. The inferred schema is closed
(`additionalProperties: false`, every non-`omitempty` field required), so a
field added or removed in a deploy is refused by every client connected before
it, until it reconnects. `WithOpenOutputSchemas` publishes the output schemas
open at every level, keeping types and descriptions:

```go
gomcp.NewServer("shop", "v1", tools, gomcp.WithOpenOutputSchemas())
```

## What a failed call shows

A handler error becomes a tool result with `isError: true`, so the model reads
it and can correct itself; it is never a protocol error. By default the
message is `err.Error()`. `WithErrorPolicy` decides what the client reads —
typically your business errors as they are and everything else masked:

```go
func showOnlyBusinessErrors(err error) string {
    if appErr, ok := apperr.AsAppError(err); ok && !apperr.IsInternal(err) {
        return appErr.Message()
    }
    return "internal error"
}
```

`PostInvoke` always receives the original error, before the policy rewrites
it, which is where you log it.

A panic in a tool or in `PreInvoke` is recovered: it becomes an error wrapping
`ErrToolPanicked`, with its stack, so the call fails and the server keeps
serving.

## Serving it

`Handler` serves Streamable HTTP in stateless mode with JSON responses: no
`Mcp-Session-Id`, no session store, each POST stands alone. That is the only
mode in which the go-sdk serves protocol 2026-07-28; older clients still
negotiate an earlier revision on the same endpoint.

Requests are checked in this order:

1. **Origin.** A request carrying an `Origin` header not listed in
   `HandlerOptions.AllowedOrigins` answers 403. Same-host is not enough: a page
   under DNS rebinding shares your Host. A request without `Origin`, as a
   non-browser client sends, passes to the next check.
2. **Bearer.** No token, or one the verifier rejects, answers 401 with
   `WWW-Authenticate`. Set `ResourceMetadataURL` to announce the RFC 9728
   document in that header.
3. **Host on loopback.** The go-sdk refuses with 403 a request that reached a
   loopback listener under a non-loopback Host.

### Echo v5

```go
e.Any("/mcp", echo.WrapHandler(handler))
```

### Behind a tunnel

A tunnel (cloudflared, ngrok) forwards a public Host to your loopback listener,
which the DNS-rebinding guard refuses with 403. Behind one, set
`HandlerOptions.DisableLocalhostProtection: true`; the bearer and the Origin
allowlist still apply.

### Protected resource metadata (RFC 9728)

An OAuth client discovers the authorization server from the document the
`WWW-Authenticate` header points to. Serve it with the go-sdk's handler on the
same URL you put in `ResourceMetadataURL`:

```go
const resource = "https://api.example.com/mcp"

e.GET("/.well-known/oauth-protected-resource/mcp", echo.WrapHandler(
    auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
        Resource:             resource,
        AuthorizationServers: []string{"https://api.example.com"},
    })))

handler, err := gomcp.Handler(server, gomcp.HandlerOptions{
    Verifier:            verifyToken,
    ResourceMetadataURL: "https://api.example.com/.well-known/oauth-protected-resource/mcp",
})
```

## Connecting Claude Code

```sh
claude mcp add --transport http financial-control http://localhost:5000/mcp \
  --header "Authorization: Bearer fcm_..."
```

## License

MIT — see [LICENSE](LICENSE).
