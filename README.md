# 物簿 Life Ledger v0.1

一次现实事件，同时更新钱、物、库存与历史。基于 [Timo348/Stockroom](https://github.com/Timo348/Stockroom) 的 Go + SQLite 定向扩展，保留母体库存、Intake、媒体、Docker 和原测试。

**朝汐 ZhaoXi 项目严格只读。** 兼容工具包在本仓库 `tools/wubu_tool`，已验证 SDK / HTTP 契约，但未安装或激活到朝汐实例。见 [接入说明](docs/ZHAOXI_INTEGRATION.md)。

## 启动

Windows 双击 `启动物簿.cmd`，或在本目录运行：

```powershell
.\scripts\start.ps1
```

打开 http://127.0.0.1:8026 。Ctrl+C 停止，数据保存在 `data/wubu.db`，停止/重启不丢失。默认只监听本机，不需要配置 AI。Go 工具链可来自系统（Go >=1.25），或本机已准备且不入 Git 的 `.tools/go`；首次构建需下载 go.mod 锁定依赖。已构建时可 `.\scripts\start.ps1 -NoBuild`。端口可通过 `-Port 8027` 调整。

Linux / macOS：

```sh
go run ./cmd/stockroom
```

Docker（本次环境无 Docker，未运行容器验收）：

```sh
docker compose up -d --build
```

容器内监听 8080，宿主机默认 8026；SQLite 数据卷持续保存。旧 Stockroom 数据库可通过 `DATABASE_PATH` 指向原文件，应用会升级至 schema 7，保留原 Item 并生成库存基线历史。迁移前先备份原数据库；本仓库的正式库与验收库分开。

## 已实现

- 混合购买：CONSUMABLE 增库存，DURABLE 创建独立实例，UNTRACKED 只记费用；购买、费用、商品、库存历史在同一事务。
- 用、调、开、完、弃：复用库存 Store / 原 adjust 路径，当前状态和事件历史原子写入；旧 CRUD、Intake 和七天自动耗尽也留下历史。
- 耐用品品牌、型号、序列号、保修、状态与售出信息；维护费用和 Expense 同一事务。
- 永久持久化 Idempotency-Key：重放原响应、不同输入返回 409；失败可用原键重试；source + external_id 去重。
- 名称 / 别名 / 标准化精确匹配；重复名称拒绝猜测；价格查询直接来自 PurchaseItem。
- 购买、费用、维护与库存历史查询，分页/日期筛选，上海时区月支出汇总。
- 首页、库存、物品、购买、消费、维护、历史，购买表单与事件纠错，保留原库存编辑 UI。
- 全部新增 REST 进入 OpenAPI 3.1；物簿侧朝汐 SDK 工具包与导入 Draft 边界。

金额 API 使用 **CNY 元**（最多两位小数），SQLite 存整数分。原 Item.unit_price_cents 仍为分。只支持 CNY；实际付款允许不同于商品合计；耐用品每件一行、数量 1；不实现收入账本、汇率转换、真实支付宝/微信 CSV 适配或额外 LLM 系统。

## API 快速验收

```powershell
$body = Get-Content .\docs\examples\purchase.json -Raw -Encoding utf8
Invoke-RestMethod http://127.0.0.1:8026/api/v1/purchases `
  -Method Post -ContentType 'application/json; charset=utf-8' `
  -Headers @{ 'Idempotency-Key' = 'example-purchase-001' } `
  -Body ([Text.Encoding]::UTF8.GetBytes($body))
```

同一键与同一输入重复请求只成功一次。修改过输入后不要复用此键。此命令会真实写入当前账簿，示例数据建议对独立测试库执行。

- API 索引：`GET /api/v1`
- 健康检查：`GET /healthz`
- 契约：`GET /api/v1/openapi.json`
- 事件入口：`POST /api/v1/purchases`，`POST /api/v1/items/{id}/consume|adjust|open|finish|discard|maintenance`
- 汇总：`GET /api/v1/ledger/summary?month=2026-10`

时间输入为带时区 RFC3339，存 UTC；`from` 含边界、`to` 不含边界；月汇总按 Asia/Shanghai。购买或生命周期已引用的 Item 不能硬删除，使用 finish、discard 或 durable 状态保留历史。原 Intake 仅调整库存，不替代 purchases。

## 验证与文档

```powershell
.\scripts\test.ps1
# 可选：只读验证真实朝汐 SDK（-B 禁止缓存写回）
.\scripts\test.ps1 -ZhaoxiSource 'E:\桌面\改变世界的魔法\朝汐ZhaoXi\src'
```

测试覆盖任务书 A–F、混合购买、回滚、并发幂等、负库存、价格/别名、开封/耗尽、维护、旧数据库迁移、旧库存入口和 HTTP / OpenAPI。详见 [验收记录](docs/WUBU_V0.1_ACCEPTANCE.md)、[母体审计](docs/WUBU_STOCKROOM_AUDIT.md)、[开发任务书](docs/物簿%20v0.1%20开发任务书｜Stockroom%20母体版.md)。

本项目保留 Apache-2.0 [LICENSE](LICENSE)、[第三方声明](THIRD_PARTY_NOTICES.md) 和 [母体原 README](docs/STOCKROOM_UPSTREAM_README.md)。不删母体 AI / 媒体实现，后续 review 流程可继续复用。
