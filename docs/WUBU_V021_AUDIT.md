# 物簿 v0.2.1 Account Classes 审计

审计基线：v0.2 提交 `6dca89b`，2026-10-10。按任务书 Phase A 检查了账户、流水、余额汇总、购买、退款、工具与页面。

| 基线实现 | v0.2.1 扩展位置 |
|---|---|
| `internal/ledger/money.go` Account 的 type 描述账户形态，尚无资金边界 | 新增独立 account_class、可选 merchant；保留 type 并增加 MERCHANT_BALANCE |
| `internal/store/money.go` 账户余额为统一流水 delta 的缓存；信用卡净余额，额度不计资金 | 保留事实源与信用卡规则；新增 liquid / consumption 汇总，available 兼容字段指向 liquid |
| `internal/store/money_schema.go` schema 10；expenses 已是只读视图 | 冻结旧迁移，新增 account_classes_schema.go 的 schema 10→11 迁移 |
| `ledgerTx` 同事务写余额和资金事件，并持久化稳定幂等键及响应 | 新增 TopUpAccount，全部本金转账与赠送 / 损失调整同一事务 |
| TRANSFER_OUT / TRANSFER_IN 已双向关联；收入 / 消费汇总排除转账和调整 | 充值沿用两条本金转账；超出本金部分新增 BONUS，不计普通收入或消费 |
| RecordPurchase、AmendPurchase、ValidatePurchase 解析付款账户 | 共同校验消费账户商家；空商家自动补全，冲突用明确错误阻止扣款 |
| 退款关联原支出，并允许显式到账账户 | 保留实现，新增消费账户退款与跨账户退款验收；累计净消费按原支出关联退款 |
| 前端共享购买表，退款已移到购买操作组 | 保留退款位置，新增分组账户、充值、分类编辑、详情及累计使用 |
| `tools/wubu_tool` 独立工具包，朝汐项目只读 | 扩展高层工具和 OpenAPI；python -B 从只读 SDK 导入联调 |

余额按当前分类解释，不增加账户分类历史版本。充值流水保留 reason=TOP_UP；BONUS 关联本次转入流水；到账损失使用 ADJUSTMENT / TOP_UP_VALUE_LOSS，不伪装成消费。

正式数据库不用于审计或验证。迁移验收在系统临时数据库中构造 schema 10，包含原购买、耐用品、维护、旧账户、导入、退款和幂等记录，对比升级及再次启动后的历史内容与余额。没有新增持久化验证数据库或 SQL 数据样本。
