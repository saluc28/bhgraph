# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-08-04

The first release. Everything below is new, so nothing here is a breaking change.

### Added

- `Graph`, `Node`, `Edge`, `Ref`: OpenGraph payload types, with `MarshalJSON` and a streaming
  `WriteTo`.
- `Extension` and the extension definition schema types, including `IsTraversable` on each
  relationship kind, which is the flag that governs BloodHound's pathfinding, and
  `Extension.TraversableKinds` to read back which kinds carry it.
- `Validate`, `ValidateAgainst` and `Extension.Validate`. They report every problem rather
  than the first, because a failed ingest job reports errors that are poor and asynchronous.
- `Graph.UppercaseIDs`, which normalizes object ids to match what BloodHound stores.
- `client.Sign`, the `bhesignature` HMAC chain, with fixed vectors.
- `client`: `InstallExtension`, `ListExtensions`, `DeleteExtension`, `Ingest` (the three-call
  job), `JobStatus`, `Cypher`, `ShortestPath`, and generic `Get`, `Post` and `Put`.
- `client.BodySigner`, which computes a signature while the body is produced, so an ingest can
  be serialized once into both the outgoing payload and the signature.
- Streaming ingest with `WithSpoolThreshold`. Payloads above the threshold spill to a
  temporary file instead of being held in memory. Before this, `Graph.WriteTo` streamed while
  the upload consuming it buffered everything.
- `examples/minimal` and `examples/with-extension`.

### Tested

- `httptest` coverage of the client. Every request is checked against a verifier that
  recomputes the signature the way the server does, including the query string.
- The three-call ingest sequence, its order, `Content-Type` and `X-File-Upload-Name`.
- An invalid graph or schema is refused without contacting the server.
- The spool stays in memory below the threshold, spills above it, keeps every byte in order
  across the crossing, and removes its temporary file.
- `Extension` round-trips MSSQLHound's `schema.json` without losing a field, and that schema
  passes our own `Validate`.

### Notes on BloodHound behaviour

None of these are documented upstream, and each one cost time to diagnose.

The signature covers the query string. The server validates against `request.RequestURI`,
while the client helper BloodHound ships signs `request.URL.Path`. The two agree until a
request carries a parameter, and then the server answers `401 signature digest mismatch`.
bhgraph follows the server.

Object ids are uppercased on ingest, so an id reused in its original case answers
`500 not found`.

`/api/v2/extensions` is behind the `opengraph_extension_management` feature flag, which is off
by default. With it off the route answers `404`.
