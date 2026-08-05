# bhgraph

[![Tests](https://github.com/saluc28/bhgraph/actions/workflows/test.yml/badge.svg)](https://github.com/saluc28/bhgraph/actions/workflows/test.yml)
[![Lint](https://github.com/saluc28/bhgraph/actions/workflows/lint.yml/badge.svg)](https://github.com/saluc28/bhgraph/actions/workflows/lint.yml)
[![Security](https://github.com/saluc28/bhgraph/actions/workflows/security.yml/badge.svg)](https://github.com/saluc28/bhgraph/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/saluc28/bhgraph.svg)](https://pkg.go.dev/github.com/saluc28/bhgraph)

Go library for BloodHound structured OpenGraph. It builds payloads, describes extension
definition schemas, and talks to the API with signed requests.

Only structured graphs take part in the BloodHound UI's pathfinding. No importable Go library
covers extension definition schemas or the `/api/v2/extensions` endpoint, so every collector
that wants pathfinding writes that part itself. The ones that have, including SpecterOps'
own MSSQLHound, keep it under `internal/`, where nobody else can import it.

If all you need is a generic payload,
[`gopengraph`](https://github.com/TheManticoreProject/gopengraph) already does that well and
is the smaller dependency. bhgraph covers the schema and the API side.

No dependencies outside the standard library.

```
go get github.com/saluc28/bhgraph
```

## Quick start

Build a payload and write it to disk. No credentials and no server are involved, and
BloodHound accepts a `.json` upload from the UI, so this is already a usable workflow:

```go
g := bhgraph.Graph{}
g.AddNode(bhgraph.Node{ID: "alice", Kinds: []string{"EX_Person"},
    Properties: map[string]any{"name": "ALICE@EXAMPLE.TEST"}})
g.AddNode(bhgraph.Node{ID: "repo", Kinds: []string{"EX_Repo"},
    Properties: map[string]any{"name": "INFRA@EXAMPLE.TEST"}})
g.AddEdge(bhgraph.Edge{Kind: "EX_CanWrite",
    Start: bhgraph.NodeRef("alice"), End: bhgraph.NodeRef("repo")})

g.UppercaseIDs()
if err := g.Validate(); err != nil {
    return err
}
_, err := g.WriteTo(file)
```

To install a schema and upload over the API, see [`examples/with-extension`](examples/with-extension).

## Getting a token

The API uses signed requests rather than bearer tokens, so you need a token ID and a token
key. In BloodHound, go to Settings, then My Profile, then API Key Management, then Create
Token. Copy both values. The key is only shown at creation: if you lose it, delete the token
and make a new one.

```go
c, err := client.New("http://127.0.0.1:8080", tokenID, tokenKey)
```

The signature is a chain of three HMAC-SHA-256 digests over the method, the URI, the request
date truncated to the hour, and the body. `client.Sign` is exported, so you can sign a request
this library does not wrap.

Signatures are valid for at most two hours, so the client and the server need roughly
agreeing clocks.

## The extensions endpoint is behind a feature flag

`/api/v2/extensions` is gated by `opengraph_extension_management`, which is off by default.
With the flag off the route is not registered at all, so the response is `404 resource not
found`. That sends you looking for the mistake in the path, the method, or the signature.
The Community tag on the endpoint means it exists in CE, not that it is enabled.

Turn it on under Administration, then Early Access Features, which is where BloodHound's
extension management documentation points. Over the API:

```
PUT /api/v2/features/{id}/toggle
```

after finding the id in `GET /api/v2/features`. This library turns that 404 into an error that
names the flag, since the response body does not. `Features` and `FeatureEnabled` read that
endpoint, so a collector can ask instead of inferring the answer from a failure.

The flag governs more than the endpoint. `GetShortestPath` branches on it
([`pathfinding.go:156`](https://github.com/SpecterOps/BloodHound/blob/v9.5.1/cmd/api/src/api/v2/pathfinding.go#L156)),
and with the flag off the server answers from the built-in AD and Azure kinds alone, so a path
across the edges of an installed schema comes back as `404 path not found`. A correct graph and
a switched off feature give the same answer, which is why asking is worth more than re-reading
the payload.

## Two behaviours worth knowing

The signature covers the query string, and BloodHound's own Go helper does not. The server
validates against `request.RequestURI`, which includes the query, while the signing helper in
the same repository signs `request.URL.Path`, which does not. The two agree until a request
carries a parameter, and then the server answers `401 signature digest mismatch`. The Python
client published with BloodHound's documentation signs the full URI and agrees with the
server, so the Go helper is the one that differs. This library follows the server, since the
server performs the check. Reported as
[SpecterOps/BloodHound#3098](https://github.com/SpecterOps/BloodHound/issues/3098).

Object ids are uppercased on ingest, and the documented list of uppercased values does not
mention it. BloodHound's node rules page lists `name`, `operatingsystem`, `distinguishedname`
and `environmentid` as property values that ingest uppercases. A node's `id`, which becomes
its `objectid`, is uppercased too, on the generic ingest path in `ConvertGenericNode`
([`convertors.go:36`](https://github.com/SpecterOps/BloodHound/blob/v9.5.1/cmd/api/src/services/graphify/convertors.go#L36)).
Reusing that id in its original case returns `500 not found`, which reads like a missing node
rather than a case mismatch. The `use_raw_object_id` feature flag would keep the original
case, but it ships disabled and is not user updatable, so on v9.5.1 the uppercasing applies.
`Graph.UppercaseIDs()` normalizes before the upload so both sides agree.

## Validation

A failed ingest job reports errors that are poor and asynchronous. The upload is accepted, and
whatever went wrong surfaces later, if at all. `Validate` and `ValidateAgainst` run before
anything is sent, and report every problem rather than stopping at the first:

- nodes have a non-empty id, at least one kind, and no duplicate ids
- edges have a kind, and id-matched endpoints exist among the nodes in the payload
- kind names carry the namespace declared by the schema
- every kind used in the payload is declared in the schema

The last check matters more than it looks. An undeclared kind is accepted by the ingest
endpoint and then fails to appear in the UI, with nothing to indicate why.

## Verified end to end

Against BloodHound CE v9.5.1 on 2026-08-03, with one edge kind declared non-traversable:

```
alice --MemberOf--> platform --CanWrite--> repo-infra     both traversable
alice --Watches--> repo-secret                            not traversable
```

| query | result |
|---|---|
| `alice -> repo-infra`, `only_traversable=true` | path found, 3 nodes |
| `alice -> repo-secret`, `only_traversable=true` | no path |
| `alice -> repo-secret`, `only_traversable=false` | path found, 2 nodes |

The same edge is visible or invisible to pathfinding depending on a single flag in the schema.
Declaring that flag is what installing an extension schema is for, and these three queries are
how you confirm it took effect.

## Signature test vectors

`client/sign_test.go` pins the signature against fixed vectors. They were not produced by
running this library and freezing the output, which would only prove that the code agrees with
itself. They come from executing BloodHound's own reference implementation
(`cmd/api/src/api/signature.go`, v9.5.1, Apache-2.0), which is the function the server runs to
verify incoming requests. A live server then accepted requests signed with them.

## Memory on large graphs

The signature covers the request body, so the body has to be readable in full before the
request goes out. Building it in memory would make the peak allocation proportional to the
graph, and this library would end up deciding how large a graph you can ingest.

`UploadGraph` serializes the graph once, into an `io.MultiWriter` feeding both a spool and a
`BodySigner`. Payloads below `WithSpoolThreshold` (8 MiB by default) stay in memory. Larger
ones spill to a temporary file that is removed when the upload finishes. BloodHound's server
does the same thing on the receiving side.

`BodySigner` is exported, so a caller streaming a payload this library does not build can sign
it the same way.

## Status

v0.1.0, a v0 release: the API can still change, and the CHANGELOG says when it does.

Everything described above is implemented and tested. That includes a round trip of
MSSQLHound's own `schema.json` through the `Extension` types, which is the check that would
catch a field SpecterOps declares and this library does not model.

Three things are missing on purpose, because adding them now would freeze a behaviour picked
by guessing:

- There is no retry. `POST /file-upload/start` creates a job, so retrying it blindly leaves
  orphans behind, and a policy worth having has to be decided per endpoint.
- There is no client-side rate limiting. BloodHound runs rate limit middleware, but its
  effective limits are undocumented and have not been measured here, and a throttle picked at
  random is an invented limit imposed on the caller.
- Token expiry is not handled. It is governed by the `api_key_expiration_support` feature
  flag, which was off on the instance this was built against, so reporting that error clearly
  needs an instance where it is on.

If you hit one of these, an issue describing the actual case is more useful than a patch.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
