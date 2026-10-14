# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
