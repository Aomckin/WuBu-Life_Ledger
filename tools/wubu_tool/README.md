# 物簿朝汐工具包 v0.2.1

独立的朝汐 SDK v1 ToolPackage，开发和测试均在物簿目录。朝汐项目保持只读，因此本次不安装或激活工具。

入口：`wubu_tool.package:create_package`；包 ID：`wubu-tool`；工具：`record_purchase`、`validate_purchase`、`wubu_inventory_event`，以及兼容的高级 `wubu`。常用购买直接描述 name / spec / quantity / unit / price 和稳定幂等键，后端自动创建或复用商品；返回库存结果和需核对的警告。

详见物簿仓库 `docs/ZHAOXI_INTEGRATION.md` 的接口、金额单位、幂等与后续接入说明。默认服务地址为 `http://127.0.0.1:8026`。

无独立 HTTP 依赖，使用 Python 标准库；SDK / Pydantic 由朝汐环境提供。工具构造不访问网络；写入需要调用者提供幂等键。不能通过该工具启动或修改朝汐。

在物簿根目录测试标准库客户端：

```powershell
$env:PYTHONPATH = "$PWD\tools\wubu_tool"
python -B -m unittest discover -s tools\wubu_tool\tests -p test_client.py -v
```

若只读使用现有朝汐 SDK，可运行 `scripts/test.ps1 -ZhaoxiSource <朝汐 src 的绝对路径>`，全程 `-B` 防止缓存写回朝汐。

## v0.2 Possessions & Cashflow

工具包版本 0.2.0。新增资金账户、余额、收入、普通支出、转账、退款、核对余额、维护、售出、退休、移动、耐用品状态与统计查询工具。record_purchase 增加 account_id（ID 或精确名称）、item_type 和 location，省略账户使用物簿默认账户。

例：transfer_money(from_account="银行卡", to_account="支付宝", amount=500, idempotency_key="稳定事件键")；add_maintenance(item_id="已知物品ID", title="换轴", cost=50, account_id="微信", idempotency_key="稳定事件键")。每次现实事件只调用一个高层动作，原子更新余额、库存和历史。

get_expense_summary(period="month", group_by="category") 由服务器计算，退款冲减净支出，转账不计消费。get_item_total_cost 返回购入、维护、退款、总持有成本与净成本。信用卡采用净余额，欠款为负；未指定账户承接旧账且不计当前真实资金。

真实 SDK 验收使用 scripts/test.ps1 -ZhaoxiSource 与 python -B；未安装或激活到只读朝汐环境。详见 [v0.2 交付记录](../../docs/V0.2_DELIVERY.md)。

## v0.2.1 Account Classes

create_account 增加 account_class（STORAGE / CONSUMPTION，默认 STORAGE）、merchant 和 MERCHANT_BALANCE type。update_account 编辑分类 / 商家 / 状态，不改写历史。get_accounts 支持按 account_class、merchant、active 查询；get_balance 和 get_total_balance 分开返回自由资金、消费储值。

top_up_account(from_account="微信", to_account="茶颜会员", paid_amount=100, credited_amount=110, idempotency_key="稳定充值键") 原子生成本金转账和 BONUS；充值不计消费，赠送不计普通收入。record_bonus 支持独立赠送，仅限消费账户。get_account_statistics(account_id="茶颜会员") 返回累计实付充值、赠送、消费、退款、净消费、当前余额及赠送比例。

绑定商家与购买商家冲突时返回 ACCOUNT_MERCHANT_MISMATCH，需核对账户或商家。退款默认原支付账户，可显式指定其他到账账户。累计净消费按原消费关联退款，即使退款转到另一个账户也正确冲减。

真实 SDK 链路新增 integration_v021.py；验证数据全部在系统临时数据库，朝汐始终只读。见 [v0.2.1 交付](../../docs/V0.2.1_DELIVERY.md)。

## v0.3 Living Runway

通过既有 `wubu` 调用 `create/update/cancel/skip/complete_planned_cashflow`、`get_upcoming_cashflows`、`get_living_runway`、`get/set_runway_settings`、`get_spending_by_nature`、`update_expense_nature`；计划路径使用 cashflow_id，性质补录使用 transaction_id。

居住操作：create/update/get_residences、link_residence_cost、get_residence_cost、get_relocation_cost、get_residence_durables，使用 residence_id。押金 / 代垫：record_deposit、return_deposit、forfeit_deposit、get_deposits、create_receivable、settle_receivable、cancel_receivable、get_receivables，结算路径使用 claim_id。create_receivable 是本次真实代垫，会扣款；不要对已有支出再次创建。核销代表确认损失。

`get_consumption_forecast` 使用 item_id，`get_items_running_low` 不需参数。耐用品 update_durable_details 支持 has_original_packaging、purchase_proof、estimated_resale_value、residence_id。凭证只保存本地引用，不自动上传。完整 payload 见嵌入 OpenAPI schema；所有写入继续携带稳定 idempotency_key。本轮未安装或启用到朝汐。
