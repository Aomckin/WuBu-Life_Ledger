# 物簿朝汐工具包 v1.0 RC2

通过朝汐 SDK v1 与本机物簿 REST 交互。独立物品操作和关联统一走 `wubu`，各资金接口也可独立使用。按名称记录库存可调用 `wubu_inventory_event`。

- 购入使用 `record_acquisition`。维护、售出、退货使用 `record_maintenance`、`record_sale`、`record_return`。
- 实际付款、收款、退款分别使用 `record_expense`、`record_income`、`record_finance_refund`。
- 已有支出用 `backfill_acquisition` 后补，也可单独用 `link_transaction` 关联已有记录。写入需要稳定的 `idempotency_key`。

详见 [朝汐接入](../../docs/ZHAOXI_INTEGRATION.md)。在物簿根目录运行 `scripts/test.ps1 -ZhaoxiSource <只读朝汐 src>` 验证，Python 以 `-B` 导入现有朝汐 SDK。
