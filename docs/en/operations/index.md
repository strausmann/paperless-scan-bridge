# Operations

Day-two concerns: keeping the pipeline running, finding out why it is
not, and knowing what to do when something breaks.

## Pages

- [Troubleshooting](troubleshooting.md) — symptom-driven diagnosis

## Health endpoints

| Endpoint | Meaning |
| --- | --- |
| `GET /health` | Process liveness. Answers "is the daemon up?" and nothing more. |
| `GET /ready` | Dependency readiness — scanner reachable, Paperless reachable, storage writable. |
| `GET /version` | Build version and commit, for correlating behaviour with a release. |

Liveness and readiness are deliberately separate: a daemon that is alive
but cannot reach the scanner should not be restarted by an orchestrator,
it should be reported.

!!! note "`/ready` not implemented yet"

    `GET /ready` currently returns `501 Not Implemented`. The dependency
    probes land with the scan dispatch path.

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
