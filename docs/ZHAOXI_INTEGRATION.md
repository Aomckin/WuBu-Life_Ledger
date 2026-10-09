# 朝汐接入（物簿侧交付）

朝汐 ZhaoXi 项目严格只读。本次只读取其 SDK / ToolPackage 协议，未改代码、配置、虚拟环境、Git 或数据；适配包全部位于物簿仓库。**尚未安装、启用到朝汐实例，也未启动朝汐进行对话验收。**

## 已交付

- 独立包 `tools/wubu_tool`，entry point `zhaoxi.tools: wubu`，package_id `wubu-tool`，SDK 要求 `>=1,<2`。
- 对照真实朝汐 v1.4.7 / SDK 1.1.0 的 CapabilityDeclaration、Tool、ToolResult、动态权限、safe_to_replay 和 entry point 协议。
- `wubu` 工具：按 operation 调用物簿 REST；读操作 READ、事件写入 WRITE + LOCAL_STATE；保留朝汐现有确认策略，不嵌入人格、Planner 或 Prompt 系统。
- 写操作强制输入稳定 idempotency_key；网络失败不擅自生成新键或重复写入，返回 unknown_outcome；使用原键和原参数可安全重试。
- DTO schema 从 OpenAPI 同步，`python scripts/sync-tool-schema.py` 更新；朝汐负责名称识别与结构化判断。
- 标准库 HTTP 客户端默认 `http://127.0.0.1:8026`，仅接受本机根地址，不自动启动物簿或改环境。

## 接口映射

| 现实操作 | REST |
|---|---|
| record_purchase_event | POST /api/v1/purchases |
| consume_item | POST /api/v1/items/{id}/consume，quantity=消耗量 |
| adjust_inventory | POST /api/v1/items/{id}/adjust，quantity=实际剩余 |
| open_item / finish_item / discard_item | POST /api/v1/items/{id}/open 或 finish 或 discard |
| add_maintenance_event | POST /api/v1/items/{id}/maintenance |
| search_items / resolve_item | GET /api/v1/items?search= 或 /items/resolve?name= |
| get_item / get_inventory | GET /api/v1/items/{id} 或 /inventory |
| get_purchase_history / get_price_history | GET /api/v1/items/{id}/purchases 或 prices |
| get_maintenance_history / get_inventory_history | GET /api/v1/items/{id}/maintenance 或 inventory-events |
| get_recent_purchases / get_purchase_event | GET /api/v1/purchases 或 /purchases/{id} |
| get_expenses / get_ledger_summary | GET /api/v1/expenses 或 /ledger/summary |
| get_durable_details / update_durable_details | GET / PATCH /api/v1/items/{id}/durable |
| add_product_alias / get_product_aliases | POST / GET /api/v1/items/{id}/aliases |

JSON 示例（传给 wubu）：

```json
{
  "operation": "record_purchase_event",
  "idempotency_key": "receipt-20261005-001",
  "payload": {
    "merchant": "乐尔乐",
    "purchased_at": "2026-10-05T20:30:00+08:00",
    "total_amount": 229,
    "source": "agent",
    "currency": "CNY",
    "items": [
      {"name":"无糖可乐","spec":"2L","quantity":4,"unit":"瓶","unit_price":5.5,"subtotal":22,"item_type":"CONSUMABLE"},
      {"name":"泡面","quantity":2,"unit":"包","unit_price":4,"subtotal":8,"item_type":"CONSUMABLE"},
      {"name":"耳机","quantity":1,"unit":"个","unit_price":199,"subtotal":199,"item_type":"DURABLE"}
    ]
  }
}
```

v0.1.1 常用购买优先使用独立 `record_purchase`，无需先查询或创建：

```json
{"items":[{"name":"无糖可乐","spec":"2L","quantity":1,"unit":"瓶","price":5.5}],"idempotency_key":"purchase-20261006-001"}
```

单商品仅有实际总价时可以省略 price 并填 total；总价省略时按明细合计。`validate_purchase` 参数相同但不需要幂等键，仅读取与验证，不写入。

按别名消耗优先使用独立 `wubu_inventory_event`，同一事务内解析名称并扣库存：

```json
{"action":"consume","name":"零度可乐大瓶","quantity":1,"idempotency_key":"consume-20261006-001"}
```

`adjust` 的 quantity 是实际剩余；`open` / `finish` / `discard` 不填数量。不存在时不创建，多候选返回 `ITEM_AMBIGUOUS` 和 id / name / spec / unit / current_quantity；需指定规格或选择 ID。工具保留完整错误及 suggested_action，不将歧义伪装成成功。

REST 对应 `/api/v1/purchases/record`、`/purchases/validate`、`/items/consume` 等；完整购买 DTO 和现有 ID 路由继续支持耐用品及改单。规格单独填 spec 或 package_size / package_unit，库存 unit（响应同时提供 inventory_unit）仍为计数单位。

## 未来解除只读限制后的接入步骤（本次未执行）

1. 启动物簿服务；健康检查 `http://127.0.0.1:8026/healthz`。
2. 使用朝汐自己的 Python 环境安装物簿侧包（此步骤会改朝汐环境，因此本次禁止执行）：`python -m pip install -e "E:\桌面\改变世界的魔法\物簿\tools\wubu_tool" --no-deps`。
3. 在朝汐工具配置加入 `ZHAOXI_TOOL_WUBU_ENABLED=true`、`ZHAOXI_TOOL_WUBU_BASE_URL=http://127.0.0.1:8026`、可选 `ZHAOXI_TOOL_WUBU_TIMEOUT_SECONDS=15`。协议默认发现但不启用，需要显式启用。
4. 重启朝汐，检查工具列表出现 record_purchase、validate_purchase、wubu_inventory_event 和 wubu，再使用任务书场景做自然语言验收。

重试规则：同一真实事件复用同一键和同一 DTO；409 幂等冲突意味着参数变更，不应自动换键重发原事件。source + external_id 还可避免同一外部订单换键重复入账。没有外部订单号时，键必须由调用方稳定保存。

## 金额、时间与生命周期约定

- Purchase / Expense / Maintenance 的金额字段为 **CNY 元**，两位小数；SQLite 存整数分。母体 Item 的 `unit_price_cents` 继续表示分。
- 当前仅支持 CNY，不进行汇率转换；实际付款允许与商品合计不同，差异超过一分钱返回 Warning；非法数值是 Error。单价不强制分摊优惠。
- 事件时间是 RFC3339，带时区；数据库统一 UTC。`from` 含边界、`to` 不含边界。月份汇总固定 Asia/Shanghai。
- 统一匹配顺序：名称与规格 → 别名 → 归一化精确匹配 → 唯一高置信候选。多个合理候选返回 409；支持空白、全角、大小写及 2L ↔ 2000ml 规格等价，库存数量不跨单位换算。
- 新耐用品每行数量必须 1，默认创建独立实例；quantity>1 请拆行。生命周期字段在 durable 接口；售出价格仅为元数据，v0.1 没有收入账本。
- 原 Intake 只应用库存建议，不能记录完整购买费用；购买必须用 purchases。

## v0.2 工具扩展

物簿侧工具包已升至 0.2.0。新增 record_income、record_expense、transfer_money、record_refund、adjust_balance、reconcile_account、add_maintenance、sell_item、retire_item、move_item、set_durable_status，以及账户/余额/资金汇总/最近流水/耐用品/成本查询。仍不修改或安装到朝汐。

record_purchase 保留短参数，新增 account_id 和商品行 item_type/location，支持直接购买独立耐用品。create_account 可用初始真实余额建账户；set_default_payment_account 后常见购买和维护不必重复传账户。

scripts/test.ps1 的真实 SDK 验收新增 integration_v02.py，在独立临时库完成购买 → 维护 → 收纳/移动 → 售出，以及转账重试、退款、收入、余额核对和服务端汇总。所有 schema 均可生成，账户余额可从 MoneyTransaction 重算。完整说明见 [v0.2 交付](V0.2_DELIVERY.md)。
