# Operations

Day-two concerns: keeping the pipeline running, finding out why it is
not, and knowing what to do when something breaks.

## Pages

- [Troubleshooting](troubleshooting.md) — symptom-driven diagnosis

## Health endpoints

| Endpoint | Meaning |
| --- | --- |
| `GET /health` | Process liveness. Answers "is the daemon up?" and nothing more. |
| `GET /ready` | Dependency readiness — profiles loaded and `sane-runtime` reachable. |
| `GET /version` | Build version and commit, for correlating behaviour with a release. |

Liveness and readiness are deliberately separate: a daemon that is alive
but cannot reach the scanner should not be restarted by an orchestrator,
it should be reported.

`GET /ready` returns `200 {"status":"ready"}` when at least one scan
profile is loaded and `sane-runtime` answers its own health check
within 3 seconds, `503` otherwise (`no_profiles_loaded` or
`sane_runtime_unreachable` — see the [API
reference](../api-reference/index.md) for the full response shape).
It does not check Paperless-ngx or storage writability; those checks
are not implemented.

## Backup and recovery

The backup architecture, retention policy, and the tested restore
runbook are documented in
[`DISASTER_RECOVERY.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/DISASTER_RECOVERY.md).

The short version: the Pi holds nothing you cannot lose. Documents live
on the Synology NAS, and the Paperless-ngx database is dumped and
backed up with restic on a schedule. Losing the Pi costs you a reflash,
not a document.

## Security posture

The threat model — trust boundaries, assumed adversaries, and what is
explicitly out of scope — is in
[`THREAT_MODEL.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/THREAT_MODEL.md).
Vulnerability reporting is covered by
[`SECURITY.md`](https://github.com/strausmann/paperless-scan-bridge/blob/main/SECURITY.md);
please use the disclosure procedure there rather than opening a public
issue.
