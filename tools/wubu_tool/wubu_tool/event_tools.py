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


def failure(exc, key=None):
    return ToolResult(success=False, content=str(exc), error=exc.code,
                      data={"error": exc.detail}, metadata={"unknown_outcome": exc.unknown_outcome, "idempotency_key": key})


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
