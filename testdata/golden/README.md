# Golden files

## `payload.json`

The exact payload that BloodHound CE v9.5.1 accepted and processed on 2026-08-03, frozen in
the form it was sent.

This file is evidence, not a snapshot of our own output. A golden file produced by our
serializer and never accepted by a server is a tautology: it proves the code still does what it
did yesterday, not that what it does is right.

Two consequences follow. Regenerate it with `go test . -update` only after a fresh acceptance
by a real instance, because regenerating it to make a failing test pass throws away the only
thing it was for. And note that the node ids are lower case, since that is what was uploaded.
The server stored them upper cased, which is what `Graph.UppercaseIDs` and the note on
`Node.ID` are about. The file records what was sent, not what the library now recommends
sending.

The graph is the one from `examples/with-extension`, with a different namespace:

```
alice --MemberOf--> platform --CanWrite--> repo-infra     traversable
alice --Watches--> repo-secret                            not traversable
```

## `mssqlhound-schema.json`

`internal/bloodhound/schema.json` from [MSSQLHound](https://github.com/SpecterOps/MSSQLHound),
SpecterOps' own OpenGraph collector and the only public reference implementation of an
extension definition schema. Apache-2.0.

It is here to be round-tripped through our `Extension` types. If SpecterOps declares a field we
do not model, the re-serialized document loses it and
`TestExtensionRoundTripsMSSQLHoundSchema` fails. A hand-written golden file could only confirm
our own assumptions, while this one can contradict them.

It also pins the shape the project spec records (7 node kinds, 35 relationship kinds, 22 of
them traversable) and checks that their schema passes our `Validate`. If it stopped passing,
the rule to fix would be ours rather than theirs.

Refresh it when MSSQLHound changes, and read the diff: a new field there is a field we are
missing.
