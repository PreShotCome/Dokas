# Evidence master-key rotation

Evidence PDFs are encrypted at rest with per-account envelope encryption
(`internal/evidence/cipher.go`): a 32-byte **master key**
(`EVIDENCE_ENCRYPTION_KEY`) wraps each account's data-encryption key (DEK),
stored in `account_evidence_keys.wrapped_dek`; the DEK encrypts the PDF.

**The master key is write-once-do-not-change** — unless you rotate it the
way described here. If the secret is replaced with a fresh value and the old
value is gone, every existing DEK becomes un-unwrappable ("unwrap account
key: message authentication failed" at the drill `report` step) and that
evidence is permanently unrecoverable. This is the same failure the app hits
with an *ephemeral* key (unset `EVIDENCE_ENCRYPTION_KEY`).

## Back up the current key

`EVIDENCE_ENCRYPTION_KEY` lives only in Fly secrets, which do not expose the
value after it is set — it is a single point of failure. Copy it into your
password manager / KMS now:

```
flyctl ssh console -a dokaz -C 'sh -c "echo $EVIDENCE_ENCRYPTION_KEY"'
```

(Do this yourself; treat the output as a secret.) The same applies to
`EVIDENCE_SIGNING_KEY`.

## Rotating to a new master key (no data loss)

The app supports graceful rotation via a **retired-keys** list. During a
rotation the new key is active (wraps new DEKs) and the old key is retired
(still unwraps DEKs sealed under it); each DEK is re-wrapped under the active
key the first time it is accessed, or eagerly with `evidence-keys rewrap`.

1. Generate a new 32-byte base64 key (e.g. `go run ./cmd/devkeys` prints one
   as `EVIDENCE_ENCRYPTION_KEY=...`), and keep it safe.
2. Set the new key active and the **old** key retired, in one step:

   ```
   flyctl secrets set -a dokaz `
     EVIDENCE_ENCRYPTION_KEY=<NEW_KEY_B64> `
     EVIDENCE_ENCRYPTION_KEYS_RETIRED=<OLD_KEY_B64>
   ```

   `EVIDENCE_ENCRYPTION_KEYS_RETIRED` is comma-separated if you carry more
   than one prior key.
3. Migrate every DEK to the new key (otherwise migration happens lazily as
   accounts are accessed):

   ```
   flyctl ssh console -a dokaz -C '/app/evidence-keys rewrap'
   # rewrapped=<n> poison=<n>
   ```
4. Confirm nothing is left on the retired key:

   ```
   flyctl ssh console -a dokaz -C '/app/evidence-keys verify'
   # active=<all> retired=0 poison=<n>
   ```
5. Once `retired=0`, remove the retired secret:

   ```
   flyctl secrets unset -a dokaz EVIDENCE_ENCRYPTION_KEYS_RETIRED
   ```

## The `evidence-keys` ops tool

Runs on the app host (`DATABASE_URL` + keys already in the environment):

- `verify` — classify every DEK: `active` (opens with the current key),
  `retired` (opens only with a retired key — needs `rewrap`), or `poison`
  (no configured key opens it — evidence unrecoverable). Refuses to run
  against an ephemeral (unset) key so it can't mis-classify everything.
- `rewrap` — re-wrap every retired-key DEK under the active key.
- `shred-poison [--yes]` — delete DEK rows no key can unwrap. Dry-run
  without `--yes`. Only for truly-lost keys (e.g. DEKs minted under an
  ephemeral key before `EVIDENCE_ENCRYPTION_KEY` was set): the evidence is
  already gone, and the dead row blocks a fresh DEK from minting, so drills
  keep failing at `report` until it is cleared.

## Recovering from a lost key (poison rows)

If the old key is truly gone (ephemeral, or never backed up), the affected
accounts' past evidence cannot be recovered. Clear the poison rows so new
drills work again:

```
flyctl ssh console -a dokaz -C '/app/evidence-keys shred-poison'        # dry run
flyctl ssh console -a dokaz -C '/app/evidence-keys shred-poison --yes'  # delete
```

The next drill for each affected account mints a fresh DEK under the current
key and the `report` step succeeds.
