# Sample Selket evidence bundle

This is a complete, **independently verifiable** Proof-of-Recovery bundle —
the exact artifact Selket produces for a passing backup restore drill. You
can hand it to a prospect, an auditor, or an underwriter so they can see
(and check) what the evidence looks like before signing up.

| File | What it is |
|---|---|
| `evidence.pdf` | The Proof-of-Recovery report: source + SHA-256, every step, the `table_exists` assertion's expected-vs-actual result, duration, operator, and the verdict. |
| `signature.json` | The detached Ed25519 signature over the PDF's SHA-256 digest. |
| `evidence-signing-keys.pem` | The public key this sample was signed with. |

> This sample was signed with a **throwaway demo key**, not Selket's
> production signing key — so it proves the *mechanism*, not a real customer
> drill. Production evidence is signed with the persistent key published at
> `https://app.selket.io/.well-known/evidence-signing-keys.pem`.

## Verify it yourself

Download the `selket-verify` binary for your platform from the
[GitHub Releases](https://github.com/preshotcome/Soteria/releases) page
(or build it from `cmd/selket-verify` — it depends only on the Go standard
library, so there is no Selket code in your trust path), then:

```sh
selket-verify \
  --pdf=evidence.pdf \
  --sig=signature.json \
  --pubkey=evidence-signing-keys.pem
```

A `0` exit code and an `OK` line mean the signature is valid: this exact
PDF was signed by the key in the PEM, and not a byte has changed since.
Change any byte of the PDF and re-run — verification fails.

## What this does and does not claim

- **Does:** the evidence is *tamper-evident* and was produced by the holder
  of the signing key. Any modification to the PDF breaks the signature.
- **Does not (yet):** carry a third-party trusted timestamp (RFC 3161) or a
  certificate-authority document signature. The timestamp is self-asserted
  inside the signature. Those are on the roadmap; we state the current model
  plainly rather than overclaim it.
