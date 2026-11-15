# 朝汐接入

适配包位于本仓库 `tools/wubu_tool`，入口为 `wubu_tool.package:create_package`，package ID 为 `wubu-tool`，SDK 要求 `>=1,<2`。地址默认 `http://127.0.0.1:8026`。所有写入需提供稳定的 `idempotency_key`。

| 动作 | 现行工具或 operation |
| --- | --- |
| 库存 | `wubu` 的 `record_acquisition`、`amend_acquisition`、`delete_acquisition`、`record_maintenance`、`record_sale`、`record_return`。按名称消耗、调整、开封和用完用 `wubu_inventory_event`。 |
| 实际资金 | `record_expense`、`record_income`、`record_finance_refund`、`transfer_money`、`top_up_account`。退款需原支出的 `related_transaction_id`。 |
| 关联 | 用 `link_transaction` 关联现有事实，`list_domain_links` 查看，`unlink_transaction` 取消。已有支出用 `backfill_acquisition` 和 `transaction_id` 后补物品。 |
| 账单和补语义 | 原生账单在物簿网页导入。朝汐可以用 `find_transactions_near_time` 查候选，用 `annotate_transaction` 确认语义，无候选时可用 `attach_semantic_evidence`。 |

购入用商品名称、规格、数量和库存单位。改单和撤销先读 `get_acquisition` 获取当前版本。用户确认两种事实均发生时，物品与资金分别写入并各用独立幂等键，按需关联。超时保留原键和原参数重试。`acquisition_link_incomplete` 包含已提交的购入，原请求重试可补齐关联。

REST 契约在 `GET /api/v1/openapi.json`。运行 `python -B scripts/sync-tool-schema.py` 同步到 SDK，再用 `scripts/test.ps1 -ZhaoxiSource <朝汐 src>` 对临时 HTTP 服务和只读 SDK 验证。正式接入需另行安排。
