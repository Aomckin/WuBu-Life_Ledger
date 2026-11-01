"""Independent Zhaoxi SDK v1 package, implemented and stored in Wubu only."""
import asyncio
import json
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator
from zhaoxi.sdk import CapabilityDeclaration, PermissionLevel, SideEffect, Tool, ToolResult
from .client import ROUTES, WubuClient, WubuError
from .event_tools import RecordPurchaseTool, ValidatePurchaseTool, InventoryEventTool
from .reality_tools import create_reality_tools

Operation = Literal[
    'create_residence', 'update_residence', 'get_residences', 'link_residence_cost', 'get_residence_cost', 'get_relocation_cost', 'get_residence_durables', 'record_deposit', 'return_deposit', 'forfeit_deposit', 'get_deposits', 'create_receivable', 'settle_receivable', 'cancel_receivable', 'get_receivables', 'get_consumption_forecast', 'get_items_running_low',
    'create_planned_cashflow', 'update_planned_cashflow', 'cancel_planned_cashflow', 'skip_planned_cashflow', 'complete_planned_cashflow', 'get_upcoming_cashflows', 'get_living_runway', 'get_runway_settings', 'set_runway_settings', 'get_spending_by_nature', 'update_expense_nature',
    "top_up_account", "record_bonus", "update_account", "get_account_statistics", "create_account", "set_default_payment_account", "record_income", "record_expense", "transfer_money", "record_refund", "adjust_balance", "reconcile_account", "add_maintenance", "sell_item", "retire_item", "move_item", "set_durable_status", "get_accounts", "get_balance", "get_account_balance", "get_total_balance", "get_expense_summary", "get_income_summary", "get_category_summary", "get_merchant_summary", "get_recent_transactions", "get_durable_items", "get_item_total_cost",
    "amend_purchase_event", "delete_purchase_event",
    "record_purchase_event", "consume_item", "adjust_inventory", "open_item", "finish_item", "discard_item",
    "add_maintenance_event", "add_product_alias", "update_durable_details", "search_items", "resolve_item",
    "get_item", "get_inventory", "get_purchase_history", "get_price_history", "get_maintenance_history",
    "get_inventory_history", "get_recent_purchases", "get_purchase_event", "get_expenses", "get_ledger_summary",
    "get_durable_details", "get_product_aliases",
]

class WubuInput(BaseModel):
    model_config = ConfigDict(extra="forbid")
    operation: Operation
    residence_id: str | None = None
    claim_id: str | None = None
    cashflow_id: str | None = None
    transaction_id: str | None = None
    item_id: str | None = None
    purchase_id: str | None = None
    account_id: str | None = None
    idempotency_key: str | None = Field(default=None, max_length=200, description="每个现实事件一个稳定 key；超时重试保留此 key 和完全相同的 payload。")
    payload: dict[str, Any] | None = Field(default=None, description="写入 DTO；按 operation 对应的 schema 提供；金额是元，支持两位小数。")
    query: dict[str, str | int | bool] | None = Field(default=None, description="查询参数：search/name/limit/offset/from/to/category/month 等；from/to 为带时区 RFC3339。")

    @model_validator(mode="after")
    def validate_operation(self):
        method, path = ROUTES[self.operation]
        for field in ("residence_id", "claim_id", "cashflow_id", "transaction_id"):
            if "{" + field + "}" in path and not getattr(self, field):
                raise ValueError(field + " required")
        if "{item_id}" in path and not self.item_id:
            raise ValueError("item_id required; search/resolve item first")
        if "{account_id}" in path and not self.account_id:
            raise ValueError("account_id required")
        if "{purchase_id}" in path and not self.purchase_id:
            raise ValueError("purchase_id required")
        if method != "GET" and not self.idempotency_key:
            raise ValueError("write needs idempotency_key")
        if method == "GET" and self.payload is not None:
            raise ValueError("read has no payload")
        return self

    @classmethod
    def model_json_schema(cls, **kwargs):
        schema = super().model_json_schema(**kwargs)
        contract = json.loads(Path(__file__).with_name("api_schema.json").read_text(encoding="utf-8"))
        schema["$defs"] = contract["schemas"]
        schema["allOf"] = []
        for op, name in contract["inputs"].items():
            schema["allOf"].append({"if": {"properties": {"operation": {"const": op}}, "required": ["operation"]}, "then": {"properties": {"payload": {"$ref": "#/$defs/" + name}}, "required": ["payload", "idempotency_key"]}})
        return schema

class WubuTool(Tool):
    name = "wubu"
    aliases = ("物簿", "Life Ledger")
    group = "wubu"
    input_model = WubuInput
    description = (
        "物簿记录钱与物：买、用、调、开、完、修、查。购买只调用一次 record_purchase_event，"
        "不要拆成库存和账目 CRUD。写入必须有稳定 idempotency_key，超时保留原键与参数重试。"
        "常用购买优先独立 record_purchase 工具，自动匹配或创建；按名称消耗等优先 wubu_inventory_event。其他需要 ID 的操作可 resolve_item；同名耐用品不能猜。"
        "payload 金额是元（两位小数），原 Item.unit_price_cents 是分。"
        "包装规格单独写 spec：name=无糖可乐，spec=2L，quantity=1，unit=瓶；不能把规格2L误当成数量。常用单位：瓶、罐、升、毫升、个、包、盒、袋、箱、克、千克。"
        "改单或删除先 get_purchase_event 读取最新记录；提供 expected_updated_at 和新幂等键。改单保留来源、external_id 和原商品行 id；不再拆开改库存和费用。"
        "耐用品每件一行、quantity=1，服务用 UNTRACKED。只在用户有记录/查询意图时调用。"
        "库存纠正用 adjust_inventory payload.quantity（绝对剩余），消耗用 consume_item payload.quantity（用掉量）。"
        "写入 DTO 见 schema，复杂判断由朝汐完成。"
    )
    intents = ("记录购物和支出", "查询还剩多少", "记录消耗开封用完", "记录耐用品维护", "查询历史价格")
    permission = PermissionLevel.READ
    def __init__(self, client): self.client = client
    def permission_for(self, arguments):
        return PermissionLevel.READ if ROUTES.get(arguments.get("operation"), ("GET",))[0] == "GET" else PermissionLevel.WRITE
    def side_effects_for(self, arguments):
        return frozenset({SideEffect.NONE}) if self.permission_for(arguments) == PermissionLevel.READ else frozenset({SideEffect.LOCAL_STATE})
    def safe_to_replay(self, arguments):
        return self.permission_for(arguments) == PermissionLevel.READ or bool(arguments.get("idempotency_key"))
    async def execute(self, arguments):
        values = arguments.model_dump(exclude={"operation"}, exclude_none=True)
        if arguments.operation in {"top_up_account", "record_bonus", "record_purchase_event", "consume_item", "adjust_inventory", "open_item", "finish_item", "discard_item", "add_maintenance_event", "record_income", "record_expense", "transfer_money", "record_refund", "adjust_balance", "reconcile_account", "add_maintenance", "sell_item", "retire_item", "move_item", "set_durable_status"}:
            values["payload"] = {**values.get("payload", {}), "source": "agent"}
        try:
            result = await asyncio.to_thread(self.client.call, arguments.operation, **values)
            return ToolResult(success=True, content="物簿操作完成。", data=result)
        except WubuError as exc:
            return ToolResult(success=False, content=str(exc), error=exc.code, data={"error":exc.detail}, metadata={"unknown_outcome": exc.unknown_outcome, "idempotency_key": arguments.idempotency_key})

class WubuToolPackage:
    package_id = "wubu-tool"
    package_version = "0.3.0"
    requires_sdk = ">=1,<2"
    def __init__(self): self.client = None
    def capability_declaration(self): return CapabilityDeclaration(tool=True, router_hints=True)
    def configure(self, config): self.client = WubuClient(str(config.get("base_url", "http://127.0.0.1:8026")), float(config.get("timeout_seconds", 15)))
    def create_tools(self, config):
        if self.client is None: self.configure(config)
        return [WubuTool(self.client), RecordPurchaseTool(self.client), ValidatePurchaseTool(self.client), InventoryEventTool(self.client), *create_reality_tools(self.client)]
    def workflow_paths(self): return []
    def reflection_sources(self): return []
    def routing_hints(self): return [{"markers": ["物簿", "life ledger", "还剩几", "记录购买", "上次多少钱", "记录维修"], "route": "tool"}]
    def capabilities(self): return {"id": self.package_id, "name": "物簿", "description": "钱物生命周期与库存数据库", "tool": "record_purchase", "tools": ["wubu", "record_purchase", "validate_purchase", "wubu_inventory_event"] + [tool.name for tool in create_reality_tools(self.client)], "read_operations": [k for k,v in ROUTES.items() if v[0]=="GET"], "write_operations": [k for k,v in ROUTES.items() if v[0]!="GET"]}

def create_package(): return WubuToolPackage()
