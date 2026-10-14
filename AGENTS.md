# gomcp

A thin layer over `github.com/modelcontextprotocol/go-sdk` that turns typed Go
handlers into an MCP server served over stateless Streamable HTTP behind a
bearer token.

## Layout

One package, no framework: `go list -deps .` never resolves to a web
framework. Mounting on a router is shown in README.md, never imported here.

- `Tool`, `ToolDef`, the hint options — what a tool is and how it registers.
- `NewServer`, the hooks and `ErrorPolicy` — what runs around every call.
- `Caller`, `UserID` — the verified identity behind a call.
- `Handler`, `HandlerOptions` — the HTTP edge: Origin, bearer, transport.

## Commands

```sh
make test   # go test -race ./...
make lint   # golangci-lint run ./...
```

## Rules

- The go-sdk minor is pinned. Before using an SDK API, read it in the module
  cache at the pinned version; behaviour changes between minors.
- A security check is never switched off by an option: the verifier is
  required, the Origin allowlist is always applied. `DisableLocalhostProtection`
  exists only because a tunnel cannot work without it.
- A change to `Handler` is proven by a test that drives the real HTTP handler
  with the go-sdk client, never by an in-memory transport alone.
- Comments state the purpose or a non-obvious constraint, one line, two at
  most, in English; sweep `grep -n "//"` over touched files before returning.
- Tests never shape production: no field exported for a test, no test flag.
