# 2026-05-17 Zero Completion Stream Billing Fix

## Release Summary

- Project: `rootflowai-new-api`
- Branch: `main`
- Deployed source commit: `0bf7833be` (`docs: update release image tag`)
- Fix commit: `bccaacbfb` (`fix: suppress billing for empty stream responses`)
- Upstream base: `5dd0d3bcb` (`fix: add analytics placeholder (#4928)`)
- Source version: `v1.0.0-rc.6-16-g0bf7833be`
- Production baseline before release: `calciumion/new-api:v0.13.2`
- Deployed image tag: `docker.io/rootflowai/new-api:v1.0.0-rc.6-16-g0bf7833be-rootflowai-20260517`
- Imported image digest: `sha256:ae2c024097becb70aa19b980eb11b7586ced6143707cd483a81fa16a40a828c6`

This release fixes a billing bug where streaming text requests that ended without an explicit `[DONE]` and produced `completion_tokens=0` could still be settled as successful input-token consumption when prompt tokens were available.

## What Changed

- Added a text billing guard for stream responses with:
  - `is_stream=true`
  - `completion_tokens=0`
  - positive calculated quota
  - stream end reason other than explicit `done`
- Suppressed settlement quota to `0` for those abnormal empty streams.
- Kept the consume log for audit, with:
  - `quota_suppressed=true`
  - `suppressed_quota=<original calculated quota>`
  - `suppress_reason=zero_completion_stream_<reason>`
- Added unit coverage for EOF empty streams, normal `[DONE]` streams, nonzero-completion streams, and non-stream requests.

## Validation Completed

```bash
go test ./service -run 'TestShouldSuppressZeroCompletionStreamQuota|TestCalculateTextQuotaSummary|TestTiered'
```

Result: pass.

## Artifact Strategy

RootFlowAI selected plan C for this release: do not push the image to public Docker Hub or any public registry.

The image is built locally as a `linux/amd64` Docker archive, copied to every K3s service node, and imported into each node's containerd image store. Kubernetes then references the local tag with `imagePullPolicy` behavior satisfied by the node-local image.

Local archive:

```text
/private/tmp/rootflowai-new-api-v1.0.0-rc.6-16-g0bf7833be-rootflowai-20260517.tar
```

Archive SHA256:

```text
3bcb9992c6c91bfdaf8eea2c906eb226ad17f8b4886b4b4c239e498d667dda5f
```

Imported service nodes:

```text
dmit-server-1 45.59.184.79
dmit-server-2 216.234.141.46
dmit-server-3 136.175.178.49
```

Remote archive path:

```text
/tmp/new-api-rootflowai-20260517.tar
```

## Build And Import Commands

Build the archive locally:

```bash
cd /Users/ryan/WorkSpace/airelay/rootflowai-new-api

TAG=v1.0.0-rc.6-16-g0bf7833be-rootflowai-20260517
IMAGE=docker.io/rootflowai/new-api:$TAG
ARCHIVE=/private/tmp/rootflowai-new-api-$TAG.tar

docker buildx build --platform linux/amd64 -t "$IMAGE" --load .
docker save "$IMAGE" -o "$ARCHIVE"
shasum -a 256 "$ARCHIVE"
```

Copy and import on each service node:

```bash
scp -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem \
  "$ARCHIVE" root@<node-ip>:/tmp/new-api-rootflowai-20260517.tar

ssh -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem root@<node-ip> \
  'k3s ctr images import /tmp/new-api-rootflowai-20260517.tar && k3s crictl images | grep rootflowai'
```

## Deployment Commands

Deploy master and slave after the image is present on every service node:

```bash
IMAGE=docker.io/rootflowai/new-api:v1.0.0-rc.6-16-g0bf7833be-rootflowai-20260517

kubectl set image deployment/new-api-master -n airelay new-api="$IMAGE"
kubectl rollout status deployment/new-api-master -n airelay --timeout=300s

kubectl set image deployment/new-api-slave -n airelay new-api-slave="$IMAGE"
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s
```

## Post-Release Verification

API health:

```bash
curl -fsS https://api.rootflowai.com/api/status
curl -fsS https://admin.rootflowai.com/api/status
```

Current deployment:

```bash
kubectl get deploy,pods -n airelay -l app=new-api -o wide
```

Billing verification:

```sql
SELECT id, username, model_name, quota, prompt_tokens, completion_tokens,
       (other::jsonb)->>'suppress_reason' AS suppress_reason,
       to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai' AS time
FROM logs
WHERE created_at >= EXTRACT(EPOCH FROM now() - interval '30 minutes')::bigint
  AND other::text LIKE '%quota_suppressed%'
ORDER BY id DESC
LIMIT 10;
```

Confirmed after release:

```text
10361483 admin123123 claude-opus-4-7 quota=0 prompt_tokens=32690 completion_tokens=0 zero_completion_stream_eof 2026-05-17 21:15:20
10361465 admin123123 claude-opus-4-7 quota=0 prompt_tokens=16346 completion_tokens=0 zero_completion_stream_eof 2026-05-17 21:13:19
```

## Rollback Plan

Rollback to the previous production image:

```bash
PREVIOUS=calciumion/new-api:v0.13.2

kubectl set image deployment/new-api-slave -n airelay new-api-slave="$PREVIOUS"
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s

kubectl set image deployment/new-api-master -n airelay new-api="$PREVIOUS"
kubectl rollout status deployment/new-api-master -n airelay --timeout=300s
```

After rollback, confirm API health and check for new 5xx, DB migration errors, and quota settlement errors.

## Go / No-Go Criteria

Go:

- All pods Ready after rollout.
- API health returns success.
- Normal stream billing still records nonzero completion usage correctly.
- Zero-completion abnormal streams no longer decrease user quota.
- No sustained 5xx or database migration errors.

No-Go:

- Any migration blocks pod startup.
- Admin login, token creation, or user API routing breaks.
- New image is missing from any service node.
