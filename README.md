# 物簿 Life Ledger v1.0 RC2

东西有生命周期，钱有流向。基于 [Timo348/Stockroom](https://github.com/Timo348/Stockroom) 的 Go + SQLite 定向扩展，保留母体结构、库存、Intake、媒体、Docker 与 Apache-2.0 许可证。

## v1.0 Independent Domains

购入、维护、售出和退货记录物品事实。支出、收入和退款记录实际发生的款项。可按 ID 关联已有记录，也可从已有支出后补入库。网页、REST 与朝汐 SDK 均使用独立领域接口。

本地交付版本为 1.0.0-rc.2。详见 [v1.0 任务书](<docs/物簿 v1.0 开发任务书.md>)、[架构说明](docs/V1.0_ARCHITECTURE.md) 与 [迁移和验收记录](docs/V1.0_ACCEPTANCE.md)。

## v0.4 Weekly Reconciliation & Semantic Backfill

每周同步入口 `/weekly-sync` 提供微信原生 XLSX / CSV、支付宝原生 CSV 两个专用上传入口，以及 14 个银行模板。实际复用固定版本 double-entry-generator v2.16.0 的 Provider / IR；预览后提交，保留原文件、逐行证据、批次与回执。资金收件箱 `/financial-inbox` 支持歧义审核、独立语义补全、余额检查点与迟到流水补偿、已有支出的购买后补。

v0.4 基线见 [v0.4 任务书](<docs/物簿 v0.4 开发任务书｜Weekly Reconciliation & Semantic Backfill.md>)；P0 / P1 实现、验收与 P2 后续范围见 [v0.4 交付记录](docs/V0.4_DELIVERY.md)、[审计](docs/WUBU_V04_AUDIT.md)、[原生导入记录](docs/V0.4_IMPORT_RESEARCH.md)。旧任务书已废止，手机采集子项目及接入已移除。

## v0.3 Living Runway

首页现在展示未来已预留、安全可花与安全日均；可管理未来收支、固定周期和安全缓冲。生活约束页记录居住、押金与实际代垫；支出可区分日常 / 固定 / 安家 / 耐用品 / 特殊事件，库存支持历史消耗预测。

交付与计算口径见 [v0.3 交付记录](docs/V0.3_DELIVERY.md)，基线见 [v0.3 审计](docs/WUBU_V03_AUDIT.md)。本轮只在临时数据库验证，正式库及朝汐服务未迁移或重启。

## 启动

Windows 双击 `启动物簿.cmd`，或运行：

```powershell
.\scripts\start.ps1
```

打开 http://127.0.0.1:8026 。Ctrl+C 停止，正式数据在 `data/wubu.db`。默认只监听本机，不需要 AI 配置。Go >=1.25；可使用本机不入 Git 的 `.tools/go`。Linux/macOS 可运行 `go run ./cmd/stockroom`；Docker 配置保留，本次没有容器验收。

空数据库会初始化为 schema 16。已有 v0.4 数据通过 `scripts/migrate-v10.py` 从 schema 14 备份生成独立的 schema 16 文件，再安排切换。命令和回滚步骤见 [迁移和验收记录](docs/V1.0_ACCEPTANCE.md)。

## 已实现

- 账户：存储 / 消费一级分类，钱包、银行卡、现金、校园卡、信用卡、商家储值、其他形态；可绑定商家，初始余额生成事件，可设置默认付款账户、查询余额和核对真实余额。
- 资金流水：支出、收入、双向关联转账、关联退款、余额调整、受限赠送 BONUS。充值本金记转账，赠送不计普通收入；实际消费时再记支出。余额与事件同事务提交；转账不计消费或收入，退款冲减净支出。
- 购买：消耗品入库、耐用品独立实例、不跟库存的服务；购入、改单、撤销与库存事件在 Items 内提交。
- 耐用品：独立仓库、品牌型号、序列号、购买信息、位置、保修和使用时长；在用、收纳、丢失、已售、退休，完整生命周期事件。出售后可单独登记到账收入并关联。退休保留历史。
- 维护：维护独立记录，费用可选关联。历史成本继续可查。
- 查询：上海时区今日、本周、本月、本年或自定义范围；分类、商家、商品和账户统计；商品购买次数、数量、最近价格和维护次数。
- 导入：微信 / 支付宝原生专用入口和银行模板，支持逐行预览、事务提交、整文件重导提示、批次历史及原文件下载。无订单号的同秒同价多行也保留独立身份；歧义、外币、未确定的还款 / 划转进入审核。支付流水不自动生成商品库存。
- 页面：首页分开显示自由可用资金 / 消费储值，账户按分类分组，支持充值、分类编辑和账户流水详情；保留资金流水、耐用品、导入，保留原库存、购买、消费、维护与历史页面。
- Agent：现实动作高层工具，全部关键写入支持持久化 Idempotency-Key；同键同参数重放，变更参数返回 409，失败不占键。

金额 API 使用 CNY 元，最多两位小数，SQLite 存整数分。支出等 `amount` 为正的绝对值，`balance_delta` 表达方向。信用卡采用**净余额**，债务为负，转入还款增加余额，信用额度不计总资金。`liquid_balance` 是启用的存储账户资金，`consumption_balance` 是启用的消费储值，二者均排除信用卡和未指定账户。未指定账户保留无法确认账户的旧账，不猜支付钱包，也不计真实总余额。

## API 与朝汐

- 契约：`GET /api/v1/openapi.json`
- 账户：`GET / POST /api/v1/accounts`，`PATCH /api/v1/money/settings`
- 资金：`POST /api/v1/money/income|expenses|transfers|adjustments|reconcile|top-ups|bonuses`。退款使用 `POST /api/v1/finance/refunds`。
- 储值统计：`GET /api/v1/accounts/{id}/statistics`；账户列表可按 `account_class/merchant/active` 筛选。
- 查询：`GET /api/v1/money/balance|transactions|summary`，支持 `period=today|week|month|year` 或 `from/to`。
- 购入使用 `GET / POST /api/v1/acquisitions`。回填使用 `POST /api/v1/financial/transactions/{id}/acquisition`。
- 耐用品：`GET /api/v1/durables`，`POST /api/v1/items/{id}/events/sale|maintenance|return`。移动与退休保留原入口。
- 成本：`GET /api/v1/items/{id}/total-cost`
- 导入：`GET /api/v1/statements/templates`；`POST /api/v1/statements/{provider}/preview|commit`；`GET /api/v1/statements/batches|summary`。

退款需关联原支出，金额不得超过剩余未退金额。实际退货由物品页单独登记。在 `/api/v1/links` 创建、查询和取消关联。收纳仍拥有数量 1，售出和退休终止生命周期，不能直接删除物品。

**朝汐 ZhaoXi 项目严格只读。** 适配与测试在本仓库 `tools/wubu_tool`，使用 `python -B` 从现有朝汐 src 导入 SDK，没有安装、激活或修改朝汐环境。见 [接入说明](docs/ZHAOXI_INTEGRATION.md)。

## 验证

```powershell
.\scripts\test.ps1
# 可选：只读验证真实朝汐 SDK 与临时 HTTP 服务
.\scripts\test.ps1 -ZhaoxiSource 'E:\桌面\改变世界的魔法\朝汐ZhaoXi\src'
```

测试使用临时 SQLite，覆盖重启、并发幂等、转账守恒、独立退款与退货、余额重建、跨领域部分失败重试、持有成本、原生导入去重、HTTP/OpenAPI 和真实 SDK。浏览器验收脚本 `scripts/test-v02-ui.ps1` 对独立临时服务验证桌面与手机页面，需要已有 Edge 和 Playwright；可用 `-PlaywrightModule` 指定模块路径。

[v0.2.1 交付记录](docs/V0.2.1_DELIVERY.md) · [账户分类审计](docs/WUBU_V021_AUDIT.md) · [v0.2 开发审计](docs/WUBU_V02_AUDIT.md) · [v0.2 交付记录](docs/V0.2_DELIVERY.md) · [第三方声明](THIRD_PARTY_NOTICES.md) · [母体 README](docs/STOCKROOM_UPSTREAM_README.md) · [LICENSE](LICENSE)

原生账单格式、银行模板、外币与退款边界见 [原生导入记录](docs/V0.4_IMPORT_RESEARCH.md)。[旧 CSV 修复记录](docs/CSV_IMPORT_FIX.md) 仅作历史追溯，不再要求用户把微信 Excel 另存 CSV。
