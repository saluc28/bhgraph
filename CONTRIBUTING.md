# Contributing

Thanks for looking. This is a small library with a narrow scope, so the most
useful contributions are usually bug reports with a reproduction, not large
features.

## Scope

bhgraph covers four things: building OpenGraph payloads, describing extension
definition schemas, signing requests, and running the ingest job. It is not a
collector, and it does not wrap the whole BloodHound API. A pull request that
widens the scope will probably be declined, not because it is bad but because a
general client ages badly with every BloodHound release.

Two rules are unlikely to change. The first is that there are no dependencies
outside the standard library: a package that pulls in third-party code to do
HTTP and HMAC is one people avoid importing, and CI fails if `go.sum` appears.
The second is that nothing here knows what the data means. Whoever imports the
library decides what a node is.

## Getting set up

Go 1.22 or newer. There is nothing to install.

```
go build ./...
go test -race ./...
go vet ./...
gofmt -l .
```

`golangci-lint` runs in CI and its config is in `.golangci.yml`. To run it
locally, install it from https://golangci-lint.run and run `golangci-lint run`.

## Tests

Unit tests run offline and must stay that way. The HTTP client is tested against
`httptest`, with a handler that recomputes the signature the way BloodHound does
rather than just checking that a header exists.

Two files are checked against outside reality and cannot be regenerated casually.
`client/sign_test.go` pins the signature against vectors produced by running
BloodHound's own reference implementation, so regenerating them from this
library's output would only prove that the code agrees with itself.
`testdata/golden/payload.json` is a payload a real BloodHound accepted;
`go test . -update` rewrites it, but doing that without a fresh acceptance by a
live instance turns it from evidence into a copy of our own output.

End-to-end tests against a live BloodHound are manual and not in CI. See
`examples/with-extension`. Running them needs an instance with the
`opengraph_extension_management` feature flag on.

## Pull requests

Keep the diff to one subject. Unrelated cleanups in the same PR make the change
hard to review and hard to revert.

Commit messages: a short imperative subject line, then the reasoning if it is not
obvious from the diff. Explain why, not what. The diff already says what.

If a change touches the signature, the ingest sequence, or the extension schema,
say in the PR whether you tested it against a live BloodHound and which version.
Every mistake this library has made so far was in one of those areas, and none of
them showed up until a real server rejected something.

## Reporting a security issue

See [SECURITY.md](SECURITY.md). Please do not open a public issue for one.
