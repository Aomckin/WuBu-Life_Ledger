"""High-level cash flow and possession tools. All writes use one backend event."""
import asyncio
from typing import Literal
from pydantic import Field
from zhaoxi.sdk import PermissionLevel, SideEffect, Tool, ToolResult
from .event_tools import EventInput, failure
from .client import WubuError

class WriteInput(EventInput):
    idempotency_key: str = Field(min_length=1,max_length=200,description="每个现实事件一个稳定键；重试保持键和参数不变。")
    note: str = Field(default="",max_length=2000)
class PaymentInput(WriteInput):
    account_id: str = Field(default="",description="账户 ID 或精确名称；省略使用默认账户。")
    amount: float = Field(gt=0,le=10_000_000_000,description="绝对金额，元，最多两位小数。")
    occurred_at: str = Field(default="",description="可选带时区 RFC3339 时间，省略为现在。")
class IncomeInput(PaymentInput):
    category: str = Field(default="其他",max_length=100)
class ExpenseInput(IncomeInput):
    expense_nature: Literal["", "DAILY", "FIXED", "SETUP", "CAPITAL", "EXCEPTIONAL"] = ""
    merchant: str = Field(default="",max_length=200)
class TransferInput(WriteInput):
    from_account: str = Field(min_length=1,description="转出账户 ID 或精确名称。")
    to_account: str = Field(min_length=1,description="转入账户 ID 或精确名称。")
    amount: float = Field(gt=0,le=10_000_000_000)
    occurred_at: str = ""
class TopUpInput(WriteInput):
    from_account: str = Field(min_length=1,description="STORAGE 转出账户 ID 或精确名称。")
    to_account: str = Field(min_length=1,description="CONSUMPTION 储值账户 ID 或精确名称。")
    paid_amount: float = Field(gt=0,le=10_000_000_000,description="实付本金，元。充值不计消费。")
    credited_amount: float = Field(gt=0,le=10_000_000_000,description="实际到账，元；超过本金部分为赠送，少于本金返回损失提示。")
    occurred_at: str = ""
class AccountQuery(EventInput):
    account_class: Literal["STORAGE","CONSUMPTION"] | None = None
    merchant: str = Field(default="",max_length=200)
    active: bool | None = None
class AccountStatsQuery(EventInput):
    account_id: str = Field(min_length=1,description="账户 ID 或精确名称。")
class AccountEditInput(WriteInput):
    account_id: str = Field(min_length=1,description="账户 ID；编辑分类不会改写历史流水。")
    account_class: Literal["STORAGE","CONSUMPTION"] | None = None
    merchant: str | None = Field(default=None,max_length=200)
    active: bool | None = None
    name: str | None = Field(default=None,min_length=1,max_length=100)
    note: str | None = Field(default=None,max_length=2000)
class ReturnedItem(EventInput):
    item_id: str = Field(min_length=1)
    quantity: float = Field(gt=0,le=1_000_000_000)
class RefundInput(PaymentInput):
    purchase_event_id: str = Field(default="",description="原购买 ID；与 related_transaction_id 至少提供一个。")
    related_transaction_id: str = Field(default="",description="原支出 ID，支持非购买支出/维护退款。")
    return_items: list[ReturnedItem] = Field(default_factory=list,description="实际退回的商品和数量，留空仅退款，不自动猜退货。")
class BalanceInput(WriteInput):
    account_id: str = Field(min_length=1)
    actual_balance: float = Field(ge=-10_000_000_000,le=10_000_000_000,description="用户确认的真实余额；信用卡净余额可为负。")
    reason: str = Field(default="",max_length=2000)
class MaintenanceInput(WriteInput):
    item_id: str = Field(min_length=1)
    title: str = Field(min_length=1,max_length=200)
    cost: float = Field(default=0,ge=0,le=10_000_000_000)
    account_id: str = ""
    occurred_at: str = ""
class SellInput(PaymentInput):
    item_id: str = Field(min_length=1)
class ItemWriteInput(WriteInput):
    item_id: str = Field(min_length=1)
    occurred_at: str = ""
class MoveInput(ItemWriteInput):
    location: str = Field(min_length=1,max_length=100)
class StatusInput(ItemWriteInput):
    status: Literal["ACTIVE","STORED","LOST"]
class EmptyInput(EventInput):
    pass
class BalanceQuery(EventInput):
    account_id: str = Field(default="",description="账户 ID 或名称；省略查询全部资金位置。")
class RangeQuery(EventInput):
    period: Literal["today","week","month","year"] | None = None
    from_: str = Field(default="",alias="from",description="自定义范围起点 RFC3339，含边界；不能与 period 同用。")
    to: str = Field(default="",description="自定义范围终点 RFC3339，不含边界。")
class MoneyQuery(RangeQuery):
    account_id: str = ""
    category: str = ""
    merchant: str = ""
    group_by: Literal["category","merchant","account","item"] | None = None
class TransactionQuery(MoneyQuery):
    type: Literal["EXPENSE","INCOME","TRANSFER_OUT","TRANSFER_IN","REFUND","ADJUSTMENT","BONUS"] | None = None
    to_account: str = ""
    limit: int = Field(default=20,ge=1,le=500)
    offset: int = Field(default=0,ge=0)
class DurableQuery(RangeQuery):
    status: Literal["ACTIVE","STORED","LOST","SOLD","RETIRED"] | None = None
    location: str = ""
    search: str = ""
    limit: int = Field(default=100,ge=1,le=500)
    offset: int = Field(default=0,ge=0)
class CostQuery(RangeQuery):
    item_id: str = Field(min_length=1)
class AccountInput(WriteInput):
    name: str = Field(min_length=1,max_length=100)
    account_class: Literal["STORAGE","CONSUMPTION"] = "STORAGE"
    merchant: str = Field(default="",max_length=200)
    type: Literal["WALLET","BANK","CASH","CAMPUS_CARD","CREDIT_CARD","MERCHANT_BALANCE","OTHER"] = "OTHER"
    initial_balance: float = Field(default=0,ge=-10_000_000_000,le=10_000_000_000)
    credit_limit: float | None = Field(default=None,ge=0,le=10_000_000_000)
class DefaultAccountInput(EventInput):
    default_payment_account: str
    idempotency_key: str = Field(min_length=1,max_length=200)

class RealityTool(Tool):
    group = "wubu"
    def __init__(self,client,name,model,description,write=False):
        self.client=client;self.name=name;self.input_model=model;self.description=description
        self.permission=PermissionLevel.WRITE if write else PermissionLevel.READ
        self.write=write
    def side_effects_for(self,arguments): return frozenset({SideEffect.LOCAL_STATE if self.write else SideEffect.NONE})
    def safe_to_replay(self,arguments): return not self.write or bool(arguments.get("idempotency_key"))
    async def execute(self,arguments):
        values=arguments.model_dump(exclude_none=True,by_alias=True)
        key=values.pop("idempotency_key",None);item_id=values.pop("item_id",None)
        kwargs={"item_id":item_id} if item_id else {}
        if self.name in {"update_account","get_account_statistics"}: kwargs["account_id"]=values.pop("account_id")
        if self.write:
            if self.name not in {"create_account","update_account","set_default_payment_account"}: values["source"]="agent"
            kwargs.update(payload=values,idempotency_key=key)
        else:
            kwargs["query"]={k:v for k,v in values.items() if v!=""}
        try:
            result=await asyncio.to_thread(self.client.call,self.name,**kwargs)
            content=self.description.split("。")[0]+"，已完成。"
            if self.name in {"get_balance","get_total_balance"} and "liquid_balance" in result:
                content=f"自由可用资金 ¥{result['liquid_balance']:.2f}；消费储值 ¥{result['consumption_balance']:.2f}。"
            if self.name=="top_up_account":
                legs=result["transactions"];destination=legs[-1]
                content=f"充值成功：{values['from_account']} ¥{legs[0]['balance_before']:.2f} → ¥{legs[0]['balance_after']:.2f}；{values['to_account']} ¥{legs[1]['balance_before']:.2f} → ¥{destination['balance_after']:.2f}。本金 ¥{values['paid_amount']:.2f}，赠送 ¥{max(0,values['credited_amount']-values['paid_amount']):.2f}，消费 ¥0.00。"
            warnings=result.get("warnings",[])
            if warnings: content+="\n"+"\n".join(w["message"] for w in warnings)
            return ToolResult(success=True,content=content,data=result)
        except WubuError as exc: return failure(exc,key)

def create_reality_tools(client):
    definitions=[
        ("create_account",AccountInput,"创建存储或消费资金账户，可绑定消费商家；初始余额留下调整历史。信用卡采用净余额，债务为负。",True),
        ("set_default_payment_account",DefaultAccountInput,"设置默认付款账户，后续购买和维护可省略账户。",True),
        ("record_income",IncomeInput,"记录工资等收入并增加账户余额。",True),
        ("record_expense",ExpenseInput,"记录无库存意义的支出，例如打车或餐馆。",True),
        ("transfer_money",TransferInput,"从一个账户转钱到另一个账户；原子完成两条关联流水，不算消费或收入。",True),
        ("record_refund",RefundInput,"记录关联原支出的退款，恢复资金并冲减消费；实际退货时明确 return_items。",True),
        ("adjust_balance",BalanceInput,"按真实余额纠正账户，自动记录前后余额与差额。",True),
        ("reconcile_account",BalanceInput,"核对真实余额并留下调整事件。",True),
        ("add_maintenance",MaintenanceInput,"记录耐用品维护及费用，同步扣款并保留费用关联。",True),
        ("sell_item",SellInput,"出售耐用品，保存售价和售出事件并将收入写入账户。",True),
        ("retire_item",ItemWriteInput,"将耐用品退休报废，保留购买和维护历史。",True),
        ("move_item",MoveInput,"移动耐用品，记录位置变化。",True),
        ("set_durable_status",StatusInput,"记录在用、收纳或丢失状态；找到后设为 ACTIVE。",True),
        ("get_accounts",AccountQuery,"按存储 / 消费分类、商家和启用状态查询账户列表。",False),
        ("get_balance",BalanceQuery,"查询余额和钱所在的账户。",False),
        ("get_account_balance",BalanceQuery,"查询指定账户余额。",False),
        ("get_total_balance",EmptyInput,"分别查询自由可用资金与受限消费储值，避免混成一个可用数字。",False),
        ("top_up_account",TopUpInput,"给消费账户充值，本金记转账、赠送记 BONUS；原子完成且不计消费或普通收入。",True),
        ("record_bonus",PaymentInput,"记录商家赠送的会员金，仅允许消费账户，不计普通收入或消费。",True),
        ("update_account",AccountEditInput,"编辑账户分类、绑定商家或启用状态；历史资金事件不变。",True),
        ("get_account_statistics",AccountStatsQuery,"查询账户累计充值本金、赠送、消费、退款、净消费、当前余额和赠送比例。",False),
        ("get_expense_summary",MoneyQuery,"由物簿汇总支出、退款、净支出和分类，转账不计消费。",False),
        ("get_income_summary",MoneyQuery,"由物簿汇总收入，排除转账。",False),
        ("get_category_summary",MoneyQuery,"按类别汇总净支出。",False),
        ("get_merchant_summary",MoneyQuery,"按商家汇总净支出。",False),
        ("get_recent_transactions",TransactionQuery,"查询最近资金流水，可筛账户、类型和时间。",False),
        ("get_durable_items",DurableQuery,"查询耐用品仓库，可筛状态、位置、名称和购买时间。",False),
        ("get_item_total_cost",CostQuery,"查询商品累计实付、维护费、总持有成本、购买次数、数量及最近价格。",False),
    ]
    return [RealityTool(client,*definition) for definition in definitions]
