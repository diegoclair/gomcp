# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.2.0] - 2026-10-27

### Added

- `WithInstructions`: the server's `instructions`, sent in the initialize
  result for what no single tool's description can carry.
- `WithTypeSchemas`: a schema per Go type, used for every tool input field of
  that type, so a closed set of values reaches the client as an enum and the
  SDK refuses any other before the handler runs.

### Fixed

- A panicking tool no longer takes the process down: the panic becomes an
  error wrapping `ErrToolPanicked`, with its stack, that `PostInvoke` and the
  `ErrorPolicy` receive and the client reads as a tool error.

## [v0.1.1] - 2026-10-14

### Changed

- Build and CI on Go 1.27.2.
- govulncheck and Coveralls coverage upload in CI.
- README badges.

## [v0.1.0] - 2026-10-14

### Added

- `Tool[In, Out]` with the `ReadOnly`, `Destructive`, `Idempotent` and `Title`
  hints; every tool declares a closed world (`openWorldHint: false`).
- `NewServer` with `WithPreInvoke`, `WithPostInvoke` and `WithErrorPolicy`;
  the hooks receive the caller's verified token.
- `Caller(ctx)` and `UserID(ctx)` for the identity behind a tool call.
- `Handler`: stateless Streamable HTTP (protocol 2026-07-28 and the earlier
  revisions the go-sdk negotiates), JSON responses, a required bearer
  verifier, an Origin allowlist and the RFC 9728 `resource_metadata` challenge.
- Built on `github.com/modelcontextprotocol/go-sdk` v1.8.0. This replaces the
  MCP support that `github.com/diegoclair/goswag` carried up to v2.2.2.
