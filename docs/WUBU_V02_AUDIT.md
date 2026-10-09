# 物簿 v0.2 现状审计

调查基线：v0.1.1，2026-10-09。未读取或修改正式数据库；朝汐项目保持只读。

## 真实代码结构

- internal/ledger/ledger.go 定义 PurchaseInput、PurchaseLine、Expense、InventoryEvent、DurableDetails、Maintenance；金额 API 为元，Money 存整数分，时间存 UTC。
- ledger_schema.go/product_schema.go 将 Stockroom 升至 schema 9。Expense 独立表，购买与维护分别唯一引用；维护反向引用 expense_id。
- RecordPurchase 一次写购买、Expense、商品、库存事件、耐用品详情；AddMaintenance 同事务写维护与 Expense。改单直接更新 Expense，删除购买直接删除 Expense，两者高度耦合。
- ledgerTx 先取 SQLite 写锁再检查持久化幂等键；请求指纹、响应和事件同事务提交，失败不占键。购买另有 source/external_id 去重。
- 库存旧 CRUD 用 trigger 保证数量变化有事件；新动作通过 mutateInventory 标注。耐用品已有基础详情和 ACTIVE/SOLD/LOST/DISCARDED/ARCHIVED 状态，无独立生命周期事件；出售状态不产生收入。
- ledger_queries.go 从 Expense 汇总费用、从 PurchaseItem 查询价格。Dashboard 使用这些方法及库存 Summary。
- 首页 ledger.html/assets/ledger.js 提供库存、物品、购买、消费、维护、历史，没有账户及独立耐用品仓库。
- HTTP 路由为 internal/httpapi/ledger.go；OpenAPI 位于 internal/web/static/openapi.json；sync-tool-schema.py 生成工具请求契约。
- tools/wubu_tool 为物簿内独立朝汐适配，已有 record_purchase/validate_purchase/wubu_inventory_event 与高级 wubu。写入强制稳定键，不自动重试。

## 实施决定

1. MoneyTransaction 为资金唯一事实源；迁移旧费用，expenses 保留只读兼容视图，维护引用统一流水。
2. 旧记录无法确定账户时归入未指定账户，不猜付款方式；该账户不计可用余额。未配置默认账户的兼容购买使用它。
3. 信用卡采用净余额：消费减余额，债务为负，还款转入增加余额；额度不计总资金。仅 CNY、整数分，绝对金额和有符号 balance_delta 分开；调整记录 before/after/delta/reason。
4. 跨领域事件复用 ledgerTx。改单与撤销使用补偿事件保留余额轨迹，不删除资金历史。
5. 耐用品采用 ACTIVE/STORED/LOST/SOLD/RETIRED。旧数据库 ARCHIVED/DISCARDED 均迁为 RETIRED；兼容接口新收到 ARCHIVED 则按 STORED 处理，DISCARDED 按 RETIRED 处理。STORED 保留拥有数量 1；售出/退休保留 Item 和购买信息。
6. today/week/month/year 与自定义时间范围均按上海时区。退款冲减净支出，转账/调整不计收入支出；商品支出按实际付款比例分摊。
7. 导入在核心完成后提供预览/事务提交及去重，不强行生成库存。

## 验证

临时数据库测试迁移保留/重启、任务书 A–F、事务回滚、并发幂等、余额可重算、退款上限/退库存、转账守恒、生命周期和总成本。同步 API/工具契约，运行 scripts/test.ps1；真实 SDK 用 python -B 只读导入朝汐 src。
