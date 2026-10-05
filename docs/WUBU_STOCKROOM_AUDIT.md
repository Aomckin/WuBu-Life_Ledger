# Stockroom 母体审计（Phase 0）

审计日期：2026-10-05。母体： https://github.com/Timo348/Stockroom ，固定提交 `0cd0cdc110fb01deb9844948db57cd8b4900c7a1`。保留 upstream Git 历史、Apache-2.0 LICENSE 和第三方声明；开发分支 `codex/wubu-v0.1`。

## SQLite / Store

- `internal/store/sqlite.go:Open` 构造 file URI，现代纯 Go SQLite 驱动 `modernc.org/sqlite`，WAL、foreign_keys=1、busy_timeout=5000、synchronous=NORMAL；连接池 8 个连接。
- `migrate` 使用 CREATE IF NOT EXISTS + PRAGMA table_info / ensureColumn 渐进升级，当前 user_version=6；没有独立迁移框架。新增领域延续此方式，版本提升到 7，并保留旧数据库。
- 普通 Item CRUD 直接执行 SQL；Intake Apply 使用 `BeginTx` / defer Rollback / Commit。现有跨表外键主要用于 Intake 附件与分析，级联删除。
- 测试使用 `testing.T.TempDir()` 下真实临时 SQLite，不用 mock。

## Item 实际字段

`internal/inventory/inventory.go`：id、name、category、icon、quantity、unit_price_cents（可空整数分）、unit、location、low_stock_threshold、expiry_date、short_lived、auto_consume_at、notes、created_at、updated_at、version。

数量精度三位小数，上限 1e9；金额分上限 1e9；分类固定十类。ItemPatch 支持可空字段清除标志。新增 item_type / opened_at 延续现有模型。购买金额在 API 可用元输入，存储统一整数分，禁止二进制浮点直接累加账目。

## 原子 Quantity Adjust

- `internal/httpapi/server.go:adjustItem` 注册 `POST /api/v1/items/{id}/adjust`，输入 `{delta}`，域验证有限、非零数量。
- `Store.Adjust` 用单条 `UPDATE ... WHERE quantity + delta >= 0` 和 ROUND 保证原子性；更新 version、七天自动消耗安排；0 affected 区分不存在和负库存。
- HTTP 对不存在返回 404，负库存返回 409，格式/域校验返回 400/422（沿用母体错误结构）。
- 原实现没有显式事务也没有历史。物簿保留这一路径；用数据库触发器保障旧 CRUD、Intake、自动消耗所有数量变化都有历史，新事件事务补充来源、时间、购买关联。关键写入先取得 SQLite 写锁，避免读后升级锁的并发问题。

## Intake / AI / Media

- `internal/intake` 定义 Entry / Batch / Suggestion / Analysis / Workflow；`internal/media` 做媒体处理，`internal/ai` 可选环境配置模型与转录。
- HTTP 支持文本/多附件上传，创建 batch 后可安排分析；分析转换为 increase / decrease / create suggestion。
- Ready 与 Needs Review 基于置信度、名称/单位匹配、当前库存、冲突与状态判断；Apply 是显式确认，不因分析自动写库存。
- `internal/store/intake.go:ApplyIntakeAnalysis` 在单事务检查 pending、单位与重复名称，执行库存变化，标记 reviewed/applied、删除已用分析附件；批量 Apply 是逐条 Intake 原子，不是整批事务。
- 保留此流程，隐藏首页 AI 主入口。原 Intake 没有商家/总额/小计，不能冒充钱物统一购买。未来应将朝汐结构化 Purchase Draft 接入 review 后调用购买事务；v0.1 直接提供 Purchase DTO / API，既有 Intake 明确只用于库存。

## HTTP / OpenAPI / UI

- Go 标准库 ServeMux 方法+路径注册，`httpapi.New` 装配；所有静态资源由 `internal/web/embed.go` 嵌入。
- OpenAPI 是手工维护 `internal/web/static/openapi.json`（3.1.0），`GET /api/v1/openapi.json` 返回；没有生成器。新增接口、operationId、金额单位、幂等与错误必须写入此文档。
- 保留库存编辑页面，首页新增薄账簿页面提供购买、费用、维护、历史、纠错操作；不重写前端框架。

## 分阶段修改范围

1. Phase 1–3：`internal/ledger` DTO/校验，Store 迁移/交易/触发器/幂等，核心购买与回滚测试。
2. Phase 4–5：同一库存事务入口实现用/调/开/完/弃；耐用品详情与维护费用事务。
3. Phase 6–7：购买、价格、库存、维护、费用、别名与汇总查询；HTTP / OpenAPI / 集成测试。
4. Phase 8–9：在物簿目录提供朝汐接入契约与请求样例；薄 UI 和启动脚本；全量原测试、构建与验收。

朝汐 ZhaoXi 项目严格只读。接入只交付物簿侧服务与文档，不修改、不启动、不安装到朝汐目录。
