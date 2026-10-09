# Phase 12A updater trust experiment

This is a separate development-only Go module. It is not imported by Lumen,
changes no production dependencies, and makes no network request in its tests.
The fixture fetcher answers every request from memory and fails for absent or
oversized files. Signing keys are generated in memory for each test and never
written into the repository.

The experiment pins `github.com/theupdateframework/go-tuf/v2` at `v2.4.2`.
It uses the maintained client's complete refresh workflow, rather than treating
a signature on one executable as a signed release feed. Its licence is
Apache-2.0; production adoption must add its transitive licences to the
generated notices.

From the repository root, in Git Bash:

```sh
source scripts/env.sh
go -C spikes/updater test -race -count=1 -timeout 120s -v .
go -C spikes/updater vet .
```

The checks cover authenticated discovery without a runtime download, exact
product target lookup, requested payload length/hash verification, missing,
malformed, oversized, unsigned, wrongly signed and expired metadata, tampered
release identity, timestamp replay after a new client reads its persisted
cache, and root rotation requiring both the previous and replacement authority.

These fixtures establish client behaviour. They do not establish operational
key custody, live hosting, Windows publisher trust, installed activation or
database recovery. TUF metadata version checks also do not replace Lumen's
application-version, channel, architecture and schema eligibility rules.

The client's fetcher interface does not carry `context.Context`; its default
fetcher also uses `context.Background` for retry. Production integration needs
a request-scoped transport with cancellation, strict origin/redirect policy
and bounded timeouts. Each refresh requires a new client instance, with one
serial owner and the currently accepted root loaded from durable install-scoped
state. Supplying the original embedded root on every construction overwrites
the client's cached root; this experiment does not yet prove persisted root
rotation or revoked-key recovery across restart.

See [the 12A record](../../docs/phase12a-updater-2026-10-10.md) for the selected
activation direction, bootstrap contracts and remaining acceptance gates.
