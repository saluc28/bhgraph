# Security policy

## Reporting a vulnerability

Please report security issues privately through GitHub Security Advisories:
use the **Security** tab of this repository, then **Report a vulnerability**.

Do not open a public issue. Reports are usually acknowledged within a week.

## What is in scope

This library signs requests to a BloodHound instance and builds the payloads sent
to it. Three areas are worth scrutiny.

`client/sign.go` computes the `bhesignature` HMAC chain. A flaw that lets a
signature be forged, replayed outside its window, or computed over less than the
full request would be a real finding.

The rest of `client` builds and sends authenticated requests. Anything that leaks
the token key, including into an error message or a log line, is in scope.

`client/spool.go` writes large payloads to a temporary file. Predictable paths,
permissions, or files that outlive the upload are in scope.

## What is not

- Vulnerabilities in BloodHound itself. Report those to SpecterOps.
- The fact that a token key must be held in memory to sign a request. That is
  the scheme, not this implementation.
- Behaviour of a graph produced by whoever imports the library. This code does
  not decide what the data means.

## Handling of secrets

The library never logs the token key and never puts it in an error. If you find
a path where it does, that is a bug and worth reporting even if it looks minor.

Signatures are valid for at most two hours, which is a property of the scheme
rather than a choice made here.
