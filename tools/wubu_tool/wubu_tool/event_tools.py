"""Small event tools; the adapter, not the model, builds persistence DTOs."""
import asyncio
from typing import Literal
from pydantic import BaseModel, ConfigDict, Field, model_validator
from zhaoxi.sdk import PermissionLevel, SideEffect, Tool, ToolResult
from .client import WubuError


class EventInput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    @classmethod
    def model_json_schema(cls, **kwargs):
        schema = super().model_json_schema(**kwargs)
        def simplify(value):
            if isinstance(value, dict):
                if "anyOf" in value:
                    options = [v for v in value["anyOf"] if v.get("type") != "null"]
                    if len(options) == 1:
                        value.pop("anyOf")
                        value.update(options[0])
                        if value.get("default", 1) is None:
                            value.pop("default")
                for child in value.values(): simplify(child)
            elif isinstance(value, list):
                for child in value: simplify(child)
        simplify(schema)
        return schema


class PurchaseItem(EventInput):
    item_type: Literal["CONSUMABLE","DURABLE","UNTRACKED"] = "CONSUMABLE"
    location: str = Field(default="",max_length=100)
    name: str = Field(min_length=1, max_length=80, description="商品名，例如无糖可乐。")
    quantity: float = Field(gt=0, le=1_000_000_000, description="购买数量，例如 1；规格 2L 不等于数量 2。")
    unit: str = Field(min_length=1, max_length=20, description="库存计数单位，例如瓶、罐、个、升。")
    price: float | None = Field(default=None, ge=0, le=10_000_000, description="每库存单位的单价，元；如 5.5 元/瓶。")
    spec: str = Field(default="", max_length=100, description="每包装规格，例如 2L 或 900ml。")


class PurchaseInput(EventInput):
    expense_nature: Literal["", "DAILY", "FIXED", "SETUP", "CAPITAL", "EXCEPTIONAL"] = ""
    account_id: str = Field(default="",description="账户 ID 或精确名称；省略用默认账户。")
    items: list[PurchaseItem] = Field(min_length=1, max_length=500)
    total: float | None = Field(default=None, ge=0, le=10_000_000_000, description="整笔实际付款，元；省略时由明细合计。单商品仅填总价时可推导单价。")
    merchant: str = Field(default="", max_length=200)
    payment_method: str = Field(default="", max_length=100)
    note: str = Field(default="", max_length=2000)


class RecordPurchaseInput(PurchaseInput):
    idempotency_key: str = Field(min_length=1, max_length=200, description="每个现实事件一个稳定键；超时重试保留此键和原参数。")


def failure(exc, key=None):
    return ToolResult(success=False, content=str(exc), error=exc.code,
                      data={"error": exc.detail}, metadata={"unknown_outcome": exc.unknown_outcome, "idempotency_key": key})


class RecordPurchaseTool(Tool):
    name = "record_purchase"
    group = "wubu"
    input_model = RecordPurchaseInput
    description = "记录一次现实购买。自动匹配或创建商品，同时记支出、库存和历史。无需预先查询或创建。例：name=无糖可乐，spec=2L，quantity=1，unit=瓶，price=5.5。返回库存结果及金额/规格警告；有歧义时根据候选追问。"
    permission = PermissionLevel.WRITE
    def __init__(self, client): self.client = client
    def side_effects_for(self, arguments): return frozenset({SideEffect.LOCAL_STATE})
    def safe_to_replay(self, arguments): return bool(arguments.get("idempotency_key"))
    async def execute(self, arguments):
        payload = arguments.model_dump(exclude={"idempotency_key"}, exclude_none=True)
        try:
            result = await asyncio.to_thread(self.client.call, "record_purchase", payload=payload, idempotency_key=arguments.idempotency_key)
            lines = "、".join(f"{i['name']} {i.get('spec', '')} × {i.get('quantity_added', i.get('quantity', 0))}{i['unit']}（当前 {i.get('inventory_after', '—')}{i['unit']}）" for i in result["items"])
            warnings = result.get("warnings", [])
            content = f"已记录：{lines}，¥{result['expense_amount']:.2f}。"
            if warnings: content += "请核对：" + "；".join(w["message"] for w in warnings)
            return ToolResult(success=True, content=content, data=result)
        except WubuError as exc: return failure(exc, arguments.idempotency_key)


class ValidatePurchaseTool(Tool):
    name = "validate_purchase"
    group = "wubu"
    input_model = PurchaseInput
    description = "购买预检查：商品匹配、金额与规格警告，完全不写入。低置信信息可先检查再确认购买。"
    permission = PermissionLevel.READ
    def __init__(self, client): self.client = client
    def side_effects_for(self, arguments): return frozenset({SideEffect.NONE})
    def safe_to_replay(self, arguments): return True
    async def execute(self, arguments):
        try:
            result = await asyncio.to_thread(self.client.call, "validate_purchase", payload=arguments.model_dump(exclude_none=True))
            return ToolResult(success=True, content="购买预检查完成，尚未写入。", data=result)
        except WubuError as exc: return failure(exc)


class InventoryEventInput(EventInput):
    action: Literal["consume", "adjust", "open", "finish", "discard"]
    name: str = Field(min_length=1, max_length=80, description="商品名或已知别名；后端统一匹配。")
    spec: str = Field(default="", max_length=100)
    quantity: float | None = Field(default=None, ge=0, le=1_000_000_000, description="consume 为用掉数量；adjust 为实际剩余；其余不填。")
    note: str = Field(default="", max_length=2000)
    idempotency_key: str = Field(min_length=1, max_length=200, description="每个事件一个稳定键，重试保持不变。")

    @model_validator(mode="after")
    def validate_quantity(self):
        if self.action in {"consume", "adjust"}:
            if self.quantity is None or (self.action == "consume" and self.quantity <= 0):
                raise ValueError("consume/adjust 需要有效 quantity")
        elif self.quantity is not None:
            raise ValueError("open/finish/discard 不填 quantity")
        return self


class InventoryEventTool(Tool):
    name = "wubu_inventory_event"
    group = "wubu"
    input_model = InventoryEventInput
    description = "按商品名、规格或别名记录消耗、纠正剩余、开封、用完、弃置。无需先查询 ID。不存在不创建；歧义返回候选。"
    permission = PermissionLevel.WRITE
    def __init__(self, client): self.client = client
    def side_effects_for(self, arguments): return frozenset({SideEffect.LOCAL_STATE})
    def safe_to_replay(self, arguments): return bool(arguments.get("idempotency_key"))
    async def execute(self, arguments):
        payload = arguments.model_dump(exclude={"action", "idempotency_key"}, exclude_none=True)
        payload["source"] = "agent"
        try:
            result = await asyncio.to_thread(self.client.call, arguments.action+"_by_name", payload=payload, idempotency_key=arguments.idempotency_key)
            return ToolResult(success=True, content=f"已记录，当前库存 {result['item']['quantity']}{result['item']['unit']}。", data=result)
        except WubuError as exc: return failure(exc, arguments.idempotency_key)
