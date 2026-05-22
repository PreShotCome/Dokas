# Document-signing certificate

## Status — seam (documented, not implemented)

This is the one integration deliberately **not** coded: it is
security-critical, and a hand-rolled, unverified implementation would
undermine the product's core trust claim. This runbook documents the seam,
what a real implementation needs, and why the current approach is a sound
interim.

## What it powers

Tamper-evidence on the evidence PDF — the cryptographic proof that a drill's
report has not been altered since it was produced.

## Current state — Ed25519 signing (works today)

`internal/evidence` signs every evidence PDF with an **Ed25519** key:

- `EVIDENCE_SIGNING_KEY` — the active private key (PKCS#8 PEM). Set a
  persistent key in production; unset, an ephemeral key is generated.
- `EVIDENCE_VERIFICATION_KEYS` — retired public keys, so evidence signed
  before a key rotation still verifies.

This is a real cryptographic signature — the evidence **is** tamper-evident
today. Its limitation: the verifier is trusted with a **bare public key**,
not a certificate. There is no chain up to a public Certificate Authority,
so a third party can't validate the signature with standard PKI tooling.

## What a real document-signing cert adds

A certificate from a CA (e.g. DigiCert) chains the signature to a publicly
trusted root: anyone can verify the evidence with off-the-shelf tools
(Adobe, OS trust stores) without being handed a key out of band.

## Why it is a seam, not built

1. **Different signing model.** CA certs are RSA/ECDSA, not Ed25519, and
   verifiable signing means **CMS / PKCS#7 detached signatures** that embed
   the certificate chain — a different code path from the current raw
   Ed25519 signature.
2. **Key custody.** Since 2023, CA/Browser-Forum rules require document- and
   code-signing private keys to live in hardware (an HSM or a qualified
   cloud KMS). The app would sign via that KMS/HSM API, not with a local
   PEM file.
3. Building CMS + a KMS signing path **unverified**, for a legal-evidence
   feature, is exactly the risk this runbook avoids.

## How to implement it (bounded follow-up)

1. Provision an asymmetric signing key in a cloud KMS (AWS KMS / GCP KMS) or
   an HSM; obtain a document-signing certificate for its public key from a
   CA.
2. Replace `Signer.Sign` with a KMS-backed signer that produces a **CMS
   detached signature** (`SignedData`) embedding the cert chain.
3. Store the CMS blob in `evidence_signatures` instead of (or alongside) the
   raw Ed25519 signature; `Verify` validates the chain to the CA root.
4. Keep `EVIDENCE_VERIFICATION_KEYS`-style rotation by carrying the cert
   serial/fingerprint on each signature.

Until then, the Ed25519 signature + a persistent `EVIDENCE_SIGNING_KEY` is a
sound interim: evidence is genuinely signed and tamper-evident — it simply
isn't anchored to a public CA.
