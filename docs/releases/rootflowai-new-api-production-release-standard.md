# RootFlowAI New API 生产发布规范

本文档用于 `rootflowai-new-api` 合并 upstream、构建镜像、灰度、全量发布和回滚。目标是让每次发布都能复现、可审计、可回滚，避免 rc.20 发布过程中暴露出的 schema 未预检、canary 混入正式流量、节点镜像未完整分发等问题再次发生。

## 1. 适用范围

- 项目：`rootflowai-new-api`
- upstream：`QuantumNous/new-api`
- 生产命名空间：`airelay`
- 服务层节点：
  - `dmit-server-1` / `45.59.184.79`
  - `dmit-server-2` / `216.234.141.46`
  - `dmit-server-3` / `136.175.178.49`
- 正式 Deployment：
  - `new-api-master`，容器名 `new-api`，1 副本，admin NodePort `30001`
  - `new-api-slave`，容器名 `new-api-slave`，2 副本，API NodePort `30000`
- 当前默认镜像策略：不推公开 Docker Hub/GHCR；本地构建 `linux/amd64` 镜像 tar，复制到每台服务节点并导入 k3s/containerd。

基础设施信息以 `/Users/ryan/WorkSpace/airelay/config/infrastructure.yaml` 为准。如果本文档与该文件冲突，先按 `infrastructure.yaml` 执行，并更新本文档。

除非特别说明，本文档中的 `kubectl` 命令都应通过 `dmit-server-1` (`45.59.184.79`) 执行，不要假设本机 Kubernetes context 可用或正确。

## 2. 发布红线

1. 发布前必须提交本次发布范围内的代码。
2. 发布前必须确认目标仓库没有未提交改动；如果存在未提交改动，先停下来确认是否纳入本次发布。
3. 禁止带着未提交的发布范围改动直接部署。
4. 禁止为了 smoke test 调用真实付费模型接口。默认只允许 `/api/status`、Pod 日志、只读 SQL、Kubernetes 状态检查等非计费验证。
5. 任何线上写操作都必须先明确说明影响并获得用户授权，包括但不限于 `kubectl set image`、`scale`、`delete`、数据库 DDL、回滚、重启。
6. 生产数据库 schema 变更必须先做 preflight 和回滚评估，不允许在不说明风险的情况下临场补表补列。
7. canary 是否接入正式流量必须提前说明。禁止创建一个看似临时、实际被正式 Service selector 选中的 canary。
8. 默认不推公开 Docker Hub/GHCR。若需要推 registry，必须单独确认目标仓库、tag、可见性和凭据。

## 3. 发布前信息收集

每次发布先记录以下信息：

```text
Release date:
Operator:
Upstream release/tag:
Upstream commit:
Local branch:
Local HEAD:
Previous production image:
New image:
Artifact strategy:
Rollback image:
DB schema changes:
Canary mode:
```

必须执行：

```bash
cd /Users/ryan/WorkSpace/airelay/rootflowai-new-api

git status --short --branch
git log --oneline -5
git remote -v
```

Go 条件：

- 当前分支和目标 release 清楚。
- 本次改动已 commit。
- 没有未解释的 dirty worktree。
- rollback image 已知，并确认节点上可用或可重新导入。

No-Go 条件：

- 工作区有未确认改动。
- 不知道当前生产镜像。
- upstream merge 冲突未充分验证。
- rollback 路径不清楚。

## 4. 合并 upstream 后的代码验证

最少执行：

```bash
go test ./common ./relay ./relay/helper ./relay/common ./relay/channel/task/gemini ./relay/channel/task/ali ./service ./service/authz ./pkg/billingexpr ./setting/ratio_setting
bun run typecheck
bun run build
```

如果改动涉及计费、日志、渠道路由、图片、缓存、鉴权、数据库模型，必须额外补充定向测试或人工审查说明。

计费相关发布禁止只看编译结果，必须明确检查：

- quota 是否可能为负数。
- `int` / `int64` / `float64` 转换是否可能溢出或截断。
- `n`、`duration`、`max_tokens`、图片尺寸、token 数等用户可控参数是否有上限。
- 异常响应、空响应、stream EOF 是否会误扣费或反向加额度。

## 5. 数据库 schema preflight

合并 upstream 后必须主动检查是否新增表、列、索引或模型迁移逻辑。

检查项：

- 搜索新增或修改的 model/entity。
- 搜索 migration、`AutoMigrate`、DDL、`CREATE TABLE`、`ALTER TABLE`。
- 对比 upstream release note 中是否提到数据库变更。
- 在生产库执行只读 schema 查询，确认上线前缺口。

只读检查示例：

```sql
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;

SELECT table_name, column_name, data_type, column_default
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name IN ('quota_data', 'system_instances')
ORDER BY table_name, ordinal_position;
```

如果发现缺表或缺列：

1. 先写出完整 DDL。
2. 明确是否可逆、是否锁表、是否需要默认值、是否需要索引。
3. 说明是否需要先备份。
4. 获得用户确认后再执行。
5. 执行后记录实际 DDL 和验证 SQL。

rc.20 已确认需要的生产 schema：

```sql
CREATE TABLE IF NOT EXISTS system_instances (
  node_name varchar(128) PRIMARY KEY,
  info text,
  started_at bigint,
  last_seen_at bigint,
  created_at bigint,
  updated_at bigint
);

CREATE INDEX IF NOT EXISTS idx_system_instances_started_at ON system_instances (started_at);
CREATE INDEX IF NOT EXISTS idx_system_instances_last_seen_at ON system_instances (last_seen_at);
CREATE INDEX IF NOT EXISTS idx_system_instances_created_at ON system_instances (created_at);
CREATE INDEX IF NOT EXISTS idx_system_instances_updated_at ON system_instances (updated_at);

ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS use_group varchar(64) DEFAULT '';
ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS token_id bigint DEFAULT 0;
ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS channel_id bigint DEFAULT 0;
ALTER TABLE quota_data ADD COLUMN IF NOT EXISTS node_name varchar(64) DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_quota_data_use_group ON quota_data (use_group);
CREATE INDEX IF NOT EXISTS idx_quota_data_token_id ON quota_data (token_id);
CREATE INDEX IF NOT EXISTS idx_quota_data_channel_id ON quota_data (channel_id);
CREATE INDEX IF NOT EXISTS idx_quota_data_node_name ON quota_data (node_name);
```

注意：这段 DDL 是 rc.20 的历史确认项。未来发布不能假设只有这些 schema 变更。

## 6. 构建和镜像产物

统一 tag 规则：

```text
docker.io/rootflowai/new-api:<upstream-version>-rootflowai-<YYYYMMDD>-<short-commit>
```

构建示例：

```bash
cd /Users/ryan/WorkSpace/airelay/rootflowai-new-api

TAG=v1.0.0-rc.20-rootflowai-20260707-7919f105
IMAGE=docker.io/rootflowai/new-api:$TAG
ARCHIVE=/private/tmp/rootflowai-new-api-$TAG.tar

docker buildx build --platform linux/amd64 -t "$IMAGE" --load .
docker save "$IMAGE" -o "$ARCHIVE"
shasum -a 256 "$ARCHIVE"
docker image inspect "$IMAGE"
```

构建后必须记录：

- image tag
- image ID / digest
- tar 路径
- tar SHA256
- tar 大小
- 构建平台必须是 `linux/amd64`

## 7. 镜像分发到 K3s 节点

在更新 Deployment 前，必须保证每台可能调度 `new-api` Pod 的服务节点都有新镜像。

复制到三台服务节点：

```bash
scp -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem \
  "$ARCHIVE" root@45.59.184.79:/tmp/

scp -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem \
  "$ARCHIVE" root@216.234.141.46:/tmp/

scp -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem \
  "$ARCHIVE" root@136.175.178.49:/tmp/
```

远端 SHA256 必须与本地一致：

```bash
ssh -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem root@<node-ip> \
  'sha256sum /tmp/rootflowai-new-api-<tag>.tar'
```

导入：

```bash
ssh -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem root@<node-ip> \
  'k3s ctr -n k8s.io images import /tmp/rootflowai-new-api-<tag>.tar'
```

确认：

```bash
ssh -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem root@<node-ip> \
  "k3s ctr -n k8s.io images ls | grep '<tag>'"
```

No-Go 条件：

- 任一节点 SHA256 不一致。
- 任一节点未导入成功。
- 任一节点磁盘空间不足。
- 镜像 tag 和计划发布 tag 不一致。

## 8. Canary 规则

发布前必须明确选择一种 canary 模式。

### 模式 A：隔离 canary

用于验证启动、schema、日志，不接正式流量。

要求：

- canary Deployment 不使用 `app=new-api`。
- 或者 Service selector 不会选中 canary。
- 单独用 Pod IP、临时 Service 或端口转发检查。

适用场景：

- upstream 大版本合并。
- schema 不确定。
- 只想验证容器启动和日志。

### 模式 B：真实流量 canary

用于接少量真实线上流量。

要求：

- 发布前明确告诉用户该 canary 会进入正式 Service。
- 明确 selector、预期流量比例和退出方式。
- 观察窗口不少于 20 分钟。
- 观察期间不要同时进行无关发布。

必须观察：

- Pod Ready / restarts
- `/api/status`
- `SQLSTATE`
- `panic` / `fatal`
- `relation ... does not exist`
- `column ... does not exist`
- 新增 5xx 是否集中在 canary
- provider/channel 错误是否与旧版本同量级

退出 canary：

- 如果失败，先摘除 canary，再排查。
- 如果成功，全量发布后删除 canary Deployment。
- 禁止长期保留临时 canary Deployment。

## 9. 全量发布顺序

先确认当前拓扑：

```bash
ssh -i /Users/ryan/WorkSpace/airelay/config/DMIT_rsa/id_rsa.pem root@45.59.184.79 \
  'kubectl get deploy new-api-master new-api-slave -n airelay -o wide; kubectl get pods -n airelay -l app=new-api -o wide'
```

推荐顺序：

1. 更新 `new-api-master`。
2. 等 master rollout 完成。
3. 检查 master `/api/status` 和严重错误日志。
4. 更新 `new-api-slave`。
5. 等 slave rollout 完成。
6. 恢复 slave 目标副本数。
7. 清理临时 canary。
8. 做发布后验证。

命令示例：

```bash
IMAGE=docker.io/rootflowai/new-api:<tag>

kubectl set image deployment/new-api-master new-api="$IMAGE" -n airelay
kubectl rollout status deployment/new-api-master -n airelay --timeout=300s

kubectl set image deployment/new-api-slave new-api-slave="$IMAGE" -n airelay
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s
kubectl scale deployment/new-api-slave -n airelay --replicas=2
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s
```

如果存在 canary 占用了 `dmit-server-2`：

```bash
kubectl scale deployment/new-api-slave-canary -n airelay --replicas=0
kubectl scale deployment/new-api-slave -n airelay --replicas=2
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s
kubectl delete deployment/new-api-slave-canary -n airelay
```

删除 canary 前必须确认正式 `new-api-slave` 已经 `2/2 Ready`。

## 10. 发布后验证

### 10.1 拓扑

```bash
kubectl get deploy new-api-master new-api-slave -n airelay -o wide
kubectl get pods -n airelay -l app=new-api -o wide
```

期望：

- `new-api-master` 为 `1/1`
- `new-api-slave` 为 `2/2`
- 三个正式 Pod 都使用新镜像。
- 三个正式 Pod 分布在 `dmit-server-1/2/3`。
- `RESTARTS=0`，或没有发布后新增重启。
- 不存在临时 canary Deployment。

### 10.2 内部健康检查

```bash
curl -sS -m 8 -o /dev/null -w 'server1-30000 HTTP=%{http_code} TIME=%{time_total}\n' http://45.59.184.79:30000/api/status
curl -sS -m 8 -o /dev/null -w 'server2-30000 HTTP=%{http_code} TIME=%{time_total}\n' http://216.234.141.46:30000/api/status
curl -sS -m 8 -o /dev/null -w 'server3-30000 HTTP=%{http_code} TIME=%{time_total}\n' http://136.175.178.49:30000/api/status
curl -sS -m 8 -o /dev/null -w 'master-admin-30001 HTTP=%{http_code} TIME=%{time_total}\n' http://45.59.184.79:30001/api/status
```

期望全部 `HTTP=200`。

### 10.3 公网入口

```bash
curl -sS -m 12 -o /dev/null -w 'api.rootflowai.com HTTP=%{http_code} TIME=%{time_total}\n' https://api.rootflowai.com/api/status
curl -sS -m 12 -o /dev/null -w 'admin.rootflowai.com HTTP=%{http_code} TIME=%{time_total}\n' https://admin.rootflowai.com/api/status
curl -sS -m 12 -o /dev/null -w 'hk.rootflowai.com HTTP=%{http_code} TIME=%{time_total}\n' https://hk.rootflowai.com/api/status
curl -sS -m 12 -o /dev/null -w 'us.rootflowai.com HTTP=%{http_code} TIME=%{time_total}\n' https://us.rootflowai.com/api/status
```

期望全部 `HTTP=200`。

### 10.4 日志

```bash
kubectl logs -n airelay deployment/new-api-master --since=5m --tail=500 \
  | grep -Ei 'SQLSTATE|panic|fatal|relation .*does not exist|column .*does not exist'

kubectl logs -n airelay -l app=new-api,role=slave --all-containers=true --since=5m --tail=1000 \
  | grep -Ei 'SQLSTATE|panic|fatal|relation .*does not exist|column .*does not exist'
```

期望没有输出。

如果看到 provider 错误、渠道 draining、用户参数错误，要和旧版本窗口对比，不能直接归因于新版本。

### 10.5 实例心跳

如果版本包含 `system_instances`：

```sql
SELECT node_name,
       to_timestamp(last_seen_at) AT TIME ZONE 'Asia/Shanghai' AS last_seen_cst
FROM system_instances
ORDER BY node_name;
```

期望三个正式 Pod 都有近期心跳。历史 canary 记录可以存在，但对应 Deployment 不应继续存在。

## 11. 观察窗口

默认观察：

- canary：不少于 20 分钟。
- 全量发布：发布后立即验证一次，再等待 60 秒复查一次。
- 高风险版本：全量发布后继续观察 20 分钟。

观察期间记录：

```text
Time:
Pod ready/restarts:
NodePort status:
Public status:
Master severe logs:
Slave severe logs:
5xx/change notes:
DB notes:
Decision:
```

## 12. 回滚规范

回滚前先确认：

- 回滚镜像 tag。
- 回滚镜像是否已在三台服务节点。
- 当前问题是否由 schema 向前迁移导致。若新版本已经写入旧版本不兼容数据，不能盲目回滚。
- 是否需要先摘除 canary 或缩容故障 Deployment。

回滚顺序通常先 slave 后 master：

```bash
PREVIOUS=docker.io/rootflowai/new-api:<previous-tag>

kubectl set image deployment/new-api-slave new-api-slave="$PREVIOUS" -n airelay
kubectl rollout status deployment/new-api-slave -n airelay --timeout=300s

kubectl set image deployment/new-api-master new-api="$PREVIOUS" -n airelay
kubectl rollout status deployment/new-api-master -n airelay --timeout=300s
```

回滚后同样执行发布后验证。

No-Go：

- 旧版本不兼容已执行的新 schema 或新数据。
- 旧镜像未导入全部节点。
- 问题来自基础设施中断而非应用版本。

## 13. 基础设施事故和发布事故分离

发布过程中如果出现机器关机、网络不可达、Ingress 异常、数据库不可达，应先判断是否是基础设施事故。

必须分开记录：

- 应用版本问题：新镜像、新代码、新 schema、新配置导致。
- 基础设施问题：VPS 关机、节点失联、Ingress/Nginx、DNS、证书、上游网络。
- 上游供应商问题：渠道 draining、供应商 5xx、无可用渠道。
- 用户请求问题：缺少 `messages`、参数非法、鉴权失败。

不要把同一时间发生的基础设施故障自动归因到新版本。

## 14. 发布记录模板

每次发布结束后，在 `docs/releases/` 新增发布记录，至少包含：

```markdown
# <date> <version> Release

## Summary

- Upstream:
- Local branch:
- Commit:
- Previous image:
- New image:
- Archive:
- SHA256:
- Nodes imported:
- DB changes:
- Canary mode:
- Rollback image:

## Validation

- Go tests:
- Typecheck:
- Build:
- Schema preflight:
- Canary observation:
- Full rollout:

## Production Result

- master:
- slave:
- public endpoints:
- logs:
- DB heartbeat:

## Issues

- What happened:
- Impact:
- Fix:
- Prevention:

## Rollback

- Rollback image:
- Rollback command:
- Compatibility notes:
```

## 15. rc.20 发布复盘固化项

这次 rc.20 发布暴露并已固化为规则：

1. 新版本 schema 必须提前识别，不能等 canary 报错后临场补。
2. canary selector 必须明确，不允许无说明混入正式 Service。
3. 本地 tar 发布必须三台节点全部导入并校验 SHA256。
4. canary 结束后必须删除临时 Deployment。
5. provider/channel 错误要和旧版本同窗口对比，避免误判。
6. DMIT 机器关机这类基础设施事故要和应用发布问题分开复盘。
