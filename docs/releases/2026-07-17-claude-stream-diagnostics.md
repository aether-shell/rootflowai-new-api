# 2026-07-17 Claude Stream Diagnostics Release

## Summary

- Upstream: `QuantumNous/new-api` `v1.0.0-rc.21`
- Upstream commit: `bde9b2f4`
- Source branch: `codex/add-claude-stream-diagnostics`
- Source commit: `0616ae886c7048cc2a0dda0562b9f4cfe4d37fcb`
- Previous image: `docker.io/rootflowai/new-api:v1.0.0-rc.21-rootflowai-20260715-78421029`
- Previous digest: `sha256:60ffc3aed58ccddcdc684d1496a33b2175081118b974601ed651ec2b2696c191`
- New image: `docker.io/rootflowai/new-api:v1.0.0-rc.21-rootflowai-20260717-0616ae88`
- New digest: `sha256:539ae23171019824cf77109fa136a66a89e4b41b77310c82590af63371265209`
- Platform: `linux/amd64`
- Archive: `/private/tmp/rootflowai-new-api-v1.0.0-rc.21-rootflowai-20260717-0616ae88.tar` (73 MiB)
- Archive SHA256: `3eff9dc665cbb355afa9f394bdfc4d21236ca0a59711ff659ddfdfe8f916a126`
- Nodes imported: `dmit-server-1`, `dmit-server-2`, `dmit-server-3`
- DB changes: none
- Canary mode: isolated Deployment with `app=new-api-diagnostic-canary`; no Service selected it
- Rollback image: `docker.io/rootflowai/new-api:v1.0.0-rc.21-rootflowai-20260715-78421029`

## Included Change

- Record at most the first 16 normalized Anthropic SSE event labels per in-flight Claude stream.
- Emit one request-correlated diagnostic log only when final completion tokens are zero, `message_stop` was not observed, and the client did not disconnect.
- Do not retain response text, prompts, thinking content, tool parameters, or unbounded upstream event names.
- Do not change response status, retry, routing, affinity, or billing behavior.

## Validation

- Claude adapter package tests passed in an isolated Linux container.
- Release-standard Go package tests passed for `common`, relay packages, task adapters, `service`, `service/authz`, `pkg/billingexpr`, and ratio settings.
- Host frontend typecheck was terminated by the resource-constrained macOS host with exit 137; no frontend files changed.
- The production Docker build completed both default and classic frontend build stages from valid unchanged-input cache and rebuilt the Linux backend binary.
- The final image is `linux/amd64` and all three nodes matched the archive SHA256 before import.
- No database model or migration files changed; no schema operation was performed.
- No paid model smoke request was sent.

## Canary

- Deployment: `new-api-diagnostic-canary`
- Node: `dmit-server-3`
- Isolation: selector `app=new-api-diagnostic-canary`; canary Pod IP was absent from both production Service endpoint sets.
- The initial canary Pod failed because the temporary canary manifest omitted the formal Deployment's `/app/logs` `emptyDir` mount. Its log was `mkdir /app/logs: no such file or directory`.
- The canary manifest was corrected with `/data` and `/app/logs` `emptyDir` mounts. The replacement Pod reached `1/1 Ready` with zero restarts.
- Final observation exceeded 24 minutes, internal `/api/status` remained HTTP 200, and no schema, panic, fatal, missing relation, or missing column log appeared.
- The canary Deployment was deleted after all formal replicas were Ready.

## Production Result

- `new-api-master`: `1/1 Ready` on `dmit-server-1`, zero restarts.
- `new-api-slave`: `2/2 Ready`, one Pod each on `dmit-server-2` and `dmit-server-3`, zero restarts.
- Internal status: all three API NodePorts and the master admin NodePort returned HTTP 200 in immediate and delayed checks.
- Public status: `api`, `admin`, `hk`, and `us` `/api/status` endpoints returned HTTP 200 in immediate and delayed checks.
- Logs: no `SQLSTATE`, panic, fatal, missing relation, or missing column matches after rollout.
- DB heartbeat: all three new production Pod names reported current heartbeats on the read replica.
- Diagnostic sample: no natural incomplete zero-output Claude stream occurred during the immediate post-release window; evidence remains pending natural traffic.

## Rollback

- Rollback image is present on all three service nodes.
- No schema or data migration was performed.
- Roll back slave first, then master:

```bash
kubectl set image deployment/new-api-slave new-api-slave=docker.io/rootflowai/new-api:v1.0.0-rc.21-rootflowai-20260715-78421029 -n airelay
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s
kubectl set image deployment/new-api-master new-api=docker.io/rootflowai/new-api:v1.0.0-rc.21-rootflowai-20260715-78421029 -n airelay
kubectl rollout status deployment/new-api-master -n airelay --timeout=300s
```
