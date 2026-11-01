# v0.3 开工审计（2026-11-01）

- 基线 HEAD：6dca89b（v0.2），v0.2.1 是已有未提交工作区，不能称为已经合并。开工保留全部已有改动；未启动正式服务或打开正式数据库。
- Account Class：已有 STORAGE / CONSUMPTION、充值本金双腿转账、BONUS、商家核对。`Balances` 仅汇总启用 STORAGE 非信用卡账户为 liquid balance，排除 unknown；消费储值单列。
- MoneyTransaction：整数分存储，postMoney 在调用方事务内记前后余额；ledgerTx 先获取写锁并保存幂等回执。未来计划应独立建表。
- PurchaseEvent：一次购买覆盖资金、商品行和库存事件；已有改单、软删除、退款、费用分摊，不重做。
- Expense Statistics：money_queries.go 数据库聚合，既有支出兼容视图；新增 nature 不能替代 Category，历史留空。
- Durable：已有保修、序列号、位置、生命周期和出售，缺原包装、凭证及参考售价与居住关联。
- InventoryEvent：真实 CONSUME / FINISH 与 ADJUST 分离，存在 before/after/delta 与 occurred_at，可用于历史消耗估算。
- Overview：ledger summary + ledger.js 首页，当前顶层是余额/本月支出，需替换为 Runway。
- Category / Import：保留类别管理与导入幂等、真实流水语义；不根据旧类别猜性质，不从导入历史自动生成计划。
- Agent Tools：本仓库 tools/wubu_tool 的 client ROUTES、package Operation 与嵌入 api_schema；不修改或安装到朝汐。
- OpenAPI：internal/web/static/openapi.json，变动后 scripts/sync-tool-schema.py 生成适配 schema。

## 实现口径

- 全版本分 P0 / P1 / P2 依次闭环，进度见 V0.3_DELIVERY.md。
- Runway 使用上海自然日；默认固定 30 天；存在下一收入则以其日期为终点。同日收入以 1 天预算防止除零。逾期必要支出仍保留。
- 计划周期只支持每周、每月、每年或自定义天数，月末截断到目标月末；读取虚拟展开周期，不写真实流水。完成或跳过只推进一期。
- v0.3 迁移代码支持现有库直接升级，验收只用临时库；正式库迁移属于部署步骤，本次不启动正式服务来触发迁移。
