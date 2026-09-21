# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Removed

- `NOTICE`. Apache-2.0 requires propagating a notice an upstream project ships, and
  neither of the two third-party things here comes with one. Where they come from is
  recorded next to them instead, in `testdata/golden/README.md` and in
  `client/sign_test.go`.

## [0.4.0] - 2026-09-20

### Added

- `client.Delete`, alongside `Get`, `Post` and `Put`. BloodHound keys a saved query by its name,
  so a query saved again under a new name becomes a second query, and removing the first one
  took a request this library could not make.

## [0.3.0] - 2026-09-19

### Added

- `NodeKind.Info` and `RelationshipKind.Info`, with `KindInfo` and `KindInfoMarkdown`: the
  sections BloodHound shows in the Entity Panel for a kind, read from v9.5.0 and evaluated as Go
  templates from v9.7.0.
- `Extension.Validate` checks those sections the way BloodHound does: at most 100 per kind, keys
  of lowercase letters, digits, hyphens and underscores, a title, a position of 0 or more that no
  other section of the kind uses, and content that parses as a template
  (`cmd/api/src/model/graphschema.go:163` at v9.7.0). Which template functions exist is left to
  the server.

## [0.2.2] - 2026-09-13

### Fixed

- `Extension.Validate` checks the namespace the way BloodHound does: every kind name must start
  with the namespace followed by an underscore (`cmd/api/src/model/graphschema.go:508` at
  v9.7.0, and the same check in every release since v8.7.0). A namespace declared with the
  underscore already in it, such as `PTD_` for `PTD_Principal`, used to pass here and is
  refused by the server.
- The environment kind is held to the same rule, and a kind name with nothing after the prefix
  is refused.

## [0.2.1] - 2026-08-06

### Added

- `client.Features` and `client.FeatureEnabled`, which read `GET /api/v2/features`. Two flags
  decide whether the rest of this library behaves the way its documentation says, and reaching
  them meant fetching and parsing that endpoint by hand.
- `client.Feature`, the modelled flag, which carries BloodHound's own description of what each
  one does.
- `client.FeatureFlagRawObjectIDs` for `use_raw_object_id`, alongside the existing
  `FeatureFlagExtensions`.

### Changed

- A `403` from the feature flag endpoint now says that the token's role cannot read the
  application configuration, instead of repeating the bare status.
- `ShortestPath` documents that an empty result can mean the extension management flag is off
  rather than that no path exists.

### Notes on BloodHound behaviour

`opengraph_extension_management` governs more than the extensions endpoint. `GetShortestPath`
branches on it (`pathfinding.go:156` at v9.5.1), and with the flag off the server answers from
the built-in AD and Azure kinds alone, so a path across the edges of an installed schema comes
back as `404 path not found`. That reads like an absent path rather than a disabled feature,
which is the reason `FeatureEnabled` is here.

## [0.2.0] - 2026-08-05

Retracted.

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

Each of these cost time to diagnose, and they are documented upstream to different degrees.

The signature covers the query string, and BloodHound's own Go helper does not. The server
validates against `request.RequestURI` while the signing helper in the same repository signs
`request.URL.Path`. The two agree until a request carries a parameter, and then the server
answers `401 signature digest mismatch`. The Python client published with the documentation
signs the full URI and agrees with the server. bhgraph follows the server, and the mismatch is
reported as SpecterOps/BloodHound#3098.

Object ids are uppercased on ingest, in `ConvertGenericNode` on the generic path
(`convertors.go:36` at v9.5.1). The documented list of property values that ingest
uppercases does not include the node `id`, so an id reused in its original case answers
`500 not found`. The `use_raw_object_id` flag would preserve the original case, but it
ships disabled and is not user updatable.

`/api/v2/extensions` is behind the `opengraph_extension_management` feature flag, which is off
by default, and with it off the route answers `404`. This one is documented, on the extension
management page, and bhgraph turns the 404 into an error that names the flag.
