# astroplate-vault

`astroplate-vault` 是面向天文底片数字化团队的批次质量验收 HTTP 服务。它以单一状态机管理底片批次从建档、扫描仪标定、逐张采集、确定性质量评估、问题裁决与重扫、独立同行抽验到科学数据封存的完整流程。

服务将批次聚合、规范化关系投影、幂等响应、封存清单和连续哈希审计事件保存在本地 SQLite 数据库中。每个写命令必须提供 `request_id`、`expected_revision` 和 `actor`；同一 `request_id` 的成功重试返回原始结果，不同载荷复用该键会被拒绝。封存后的批次只读。

## 构建

```text
go build ./cmd/server
```

## 运行

默认仅监听高位回环地址 `127.0.0.1:19081`，数据库默认保存在 `./data/astroplate-vault.db`：

```text
go run ./cmd/server
```

可显式指定回环监听地址和数据目录：

```text
go run ./cmd/server -addr=127.0.0.1:19091 -data-dir=./data
```

未显式传入 `-addr` 时，也可通过 `PORT` 提供端口号，服务会绑定 `127.0.0.1:<PORT>`。显式 `-addr` 的优先级高于 `PORT`。服务支持 `SIGINT` 和 `SIGTERM` 优雅关闭，并在启动时执行可重复迁移、聚合投影校验和全批次审计链校验。

## 测试与自检

运行全部回归测试：

```text
go test ./...
```

真实 HTTP 自检会创建临时 SQLite 数据库，在指定监听地址上完成建档、标定、采集、评估、抽验、封存、清单核验和审计查询，然后自行退出：

```text
go run ./cmd/server -self-check -self-check-timeout=20s -addr=127.0.0.1:19081
```

## API

所有业务资源位于 `/api/v1`。主要端点如下：

- `POST /api/v1/plate-batches`：创建草稿批次。
- `GET /api/v1/plate-batches`：按状态、扫描仪、质量规则版本、创建人和标题关键字检索工作台；返回稳定游标分页、状态汇总、目录/扫描/问题工作量及下一步动作。
- `PATCH /api/v1/plate-batches/{batchID}`：以乐观并发和幂等语义修订未进入采集的草稿资料。
- `POST /api/v1/plate-batches/{batchID}/calibrations`：提交标定证据。
- `POST /api/v1/plate-batches/{batchID}/scans`：登记初始扫描或带 `supersedes_scan_id` 的重扫版本。
- `POST /api/v1/plate-batches/{batchID}/scans/{scanID}/corrections`：在采集期以完整元数据和更正理由追加扫描更正版本。
- `POST /api/v1/plate-batches/{batchID}/scans/batch`：以单个幂等命令原子登记最多 200 张扫描。
- `POST /api/v1/plate-batches/{batchID}/catalogs/precheck`：只读预检目录范围、重复项、已登记项和齐套缺口。
- `POST /api/v1/plate-batches/{batchID}/quality-evaluations`：在扫描齐套后执行规则评估。
- `GET /api/v1/plate-batches/{batchID}/quality-evaluations/preview`：按 `expected_revision` 只读预览齐套缺口、逐张结论和预计整改项。
- `POST /api/v1/plate-batches/{batchID}/issues/{issueID}/resolution`：接受问题或用直接替代版本整改。
- `POST /api/v1/plate-batches/{batchID}/issues/{issueID}/resolution-revocations`：在申请同行抽验前撤销当前有效裁决并保留完整历史。
- `POST /api/v1/plate-batches/{batchID}/issues/resolutions:batch`：原子裁决最多 100 个开放问题。
- `POST /api/v1/plate-batches/{batchID}/issues/rescan-resolution`：在单个事务和单个聚合修订内登记替代扫描、执行锁定策略复验并闭合同一扫描的全部质量问题。
- `POST /api/v1/plate-batches/{batchID}/peer-review-request`：在问题全部关闭后申请固定样本抽验。
- `POST /api/v1/plate-batches/{batchID}/peer-reviews`：由未参与采集的复核员提交抽验结论。
- `POST /api/v1/plate-batches/{batchID}/peer-reviews/evidence`：提交固定样本的逐项摘要、尺寸和位深证据并由系统派生结论。
- `GET /api/v1/plate-batches/{batchID}/peer-reviews/work-item`：按 reviewer 和 `expected_revision` 获取固定样本、扫描证据与职责分离结论。
- `POST /api/v1/plate-batches/{batchID}/peer-reviews/drafts`：为当前抽验轮次创建冻结固定样本的证据草稿。
- `GET /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}`：恢复草稿进度、逐项状态和缺失目录。
- `PUT /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}/evidence/{catalogNumber}`：幂等新增或更正单个固定样本证据，并以 `expected_draft_revision` 防止草稿覆盖。
- `POST /api/v1/plate-batches/{batchID}/peer-reviews/drafts/{draftID}/completion`：在证据齐套后由系统派生抽验结论；通过进入待封存，失败按样本生成整改问题。
- `POST /api/v1/plate-batches/{batchID}/archive`：生成稳定排序清单并冻结批次。
- `GET /api/v1/plate-batches/{batchID}`：查询批次、缺失目录、活动扫描和开放问题投影。
- `GET /api/v1/plate-batches/{batchID}/calibrations`：查询逐项标定门禁、阈值和失败原因。
- `GET /api/v1/plate-batches/{batchID}/scans/progress`：按目录区间、状态和游标查询采集进度及重扫谱系。
- `GET /api/v1/plate-batches/{batchID}/quality-results`：按规则、结论或目录筛选持久化质量结果。
- `GET /api/v1/plate-batches/{batchID}/issues`：分页查询整改队列、活动扫描版本和可裁决性。
- `GET /api/v1/plate-batches/{batchID}/peer-reviews/history`：查询抽验轮次、样本有效性和回归状态。
- `GET /api/v1/plate-batches/{batchID}/manifest/preview`：用 `expected_revision` 只读预览待封存清单摘要。
- `GET /api/v1/plate-batches/{batchID}/archive/readiness`：用当前 `expected_revision` 获取全部封存阻断项和预期清单摘要。
- `GET /api/v1/plate-batches/{batchID}/manifest`：读取封存清单。
- `GET /api/v1/plate-batches/{batchID}/manifest/verify`：核验清单摘要与审计锚点，可传 `manifest_hash`。
- `POST /api/v1/plate-batches/{batchID}/manifest/reconcile`：对已封存清单与实际成果执行缺失、多余、摘要和技术属性差异核验。
- `GET /api/v1/plate-batches/{batchID}/audit-events`：按事件类型、责任人、修订区间、命令标识和序号分页交集检索，并返回分组汇总与全链完整性诊断。
- `GET /healthz`：健康检查。

请求体必须使用 `application/json`，未知字段会被拒绝，最大请求体为 1 MiB。业务错误使用 `application/problem+json` 返回稳定的 `code`；修订冲突响应还包含 `current_revision`。
