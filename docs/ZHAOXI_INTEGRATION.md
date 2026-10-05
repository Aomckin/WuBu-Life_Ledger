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
      {"name":"无糖可乐 2L","quantity":4,"unit":"瓶","unit_price":5.5,"subtotal":22,"item_type":"CONSUMABLE"},
      {"name":"泡面","quantity":2,"unit":"包","unit_price":4,"subtotal":8,"item_type":"CONSUMABLE"},
      {"name":"耳机","quantity":1,"unit":"个","unit_price":199,"subtotal":199,"item_type":"DURABLE"}
    ]
  }
}
```

读取名称后消耗：

```json
{"operation":"resolve_item","query":{"name":"零度可乐大瓶"}}
```

```json
{"operation":"consume_item","item_id":"查询返回的 id","idempotency_key":"consume-20261005-001","payload":{"quantity":1,"source":"agent"}}
```

## 未来解除只读限制后的接入步骤（本次未执行）

1. 启动物簿服务；健康检查 `http://127.0.0.1:8026/healthz`。
2. 使用朝汐自己的 Python 环境安装物簿侧包（此步骤会改朝汐环境，因此本次禁止执行）：`python -m pip install -e "E:\桌面\改变世界的魔法\物簿\tools\wubu_tool" --no-deps`。
3. 在朝汐工具配置加入 `ZHAOXI_TOOL_WUBU_ENABLED=true`、`ZHAOXI_TOOL_WUBU_BASE_URL=http://127.0.0.1:8026`、可选 `ZHAOXI_TOOL_WUBU_TIMEOUT_SECONDS=15`。协议默认发现但不启用，需要显式启用。
4. 重启朝汐，检查工具列表出现 wubu，再使用任务书 A–E 场景做自然语言验收。

重试规则：同一真实事件复用同一键和同一 DTO；409 幂等冲突意味着参数变更，不应自动换键重发原事件。source + external_id 还可避免同一外部订单换键重复入账。没有外部订单号时，键必须由调用方稳定保存。

## 金额、时间与生命周期约定

- Purchase / Expense / Maintenance 的金额字段为 **CNY 元**，两位小数；SQLite 存整数分。母体 Item 的 `unit_price_cents` 继续表示分。
- 当前仅支持 CNY，不进行汇率转换；实际付款允许与商品合计不同。单价不含强制分摊优惠。
- 事件时间是 RFC3339，带时区；数据库统一 UTC。`from` 含边界、`to` 不含边界。月份汇总固定 Asia/Shanghai。
- 同名消耗品按精确名称 → 精确别名 → 标准化精确匹配；多结果 409，需要明确 item_id；单位或物品类型冲突 409，不自动换算。
- 新耐用品每行数量必须 1，默认创建独立实例；quantity>1 请拆行。生命周期字段在 durable 接口；售出价格仅为元数据，v0.1 没有收入账本。
- 原 Intake 只应用库存建议，不能记录完整购买费用；购买必须用 purchases。
