"""Independent domains over an isolated HTTP service via the real read-only SDK."""
import asyncio
import os
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen
import json

from wubu_tool.package import create_package


assert Path(os.environ["DATABASE_PATH"]).parent.name.startswith("wubu-dogfooding-")
base = os.environ["WUBU_TEST_BASE_URL"]
tool = next(t for t in create_package().create_tools({"base_url": base}) if t.name == "wubu")


async def call(op, **kwargs):
    result = await tool.run({"operation": op, **kwargs})
    assert result.success, (op, result)
    return result.data


async def main():
    account = await call("create_account", payload={"name": "v1 验收", "type": "WALLET", "initial_balance": 100}, idempotency_key="v1-account")
    account_id = account["id"]
    line = {"name": "v1 验收可乐", "quantity": 6, "unit": "瓶", "spec": "2L"}
    acquired = await call("record_acquisition", payload={"items": [line]}, idempotency_key="v1-acquire")
    item_id = acquired["items"][0]["item_id"]
    assert (await call("get_item", item_id=item_id))["quantity"] == 6
    assert (await call("get_account_balance", query={"account_id":account_id}))["current_balance"] == 100

    p = {"account_id": "missing", "amount": 69}
    failed = await tool.run({"operation": "record_expense", "payload": p, "idempotency_key": "v1-charge"})
    assert not failed.success
    assert (await call("get_item", item_id=item_id))["quantity"] == 6
    p["account_id"] = account_id
    charges = await call("record_expense", payload=p, idempotency_key="v1-charge")
    tx = charges["transactions"][0]["id"]
    await call("annotate_transaction", transaction_id=tx, payload={
        "item_name": "可乐", "confidence": 1, "annotated_by": "agent", "annotation_status": "CONFIRMED",
    }, idempotency_key="v1-annotate")
    for _ in range(2):
        linked = await call("backfill_acquisition", transaction_id=tx, payload={"items": [line]}, idempotency_key="v1-backfill")
        assert linked["link"]["transaction_id"] == tx
    assert (await call("get_item", item_id=item_id))["quantity"] == 12
    await call("unlink_transaction", link_id=linked["link"]["id"], idempotency_key="v1-unlink")
    assert (await call("list_domain_links", query={"transaction_id": tx}))["items"] == []
    await call("link_transaction", payload={
        "source_type": "ITEM_ACQUISITION", "source_id": acquired["id"], "transaction_id": tx, "relation": "PURCHASE",
    }, idempotency_key="v1-link")
    assert (await call("get_account_balance", query={"account_id":account_id}))["current_balance"] == 31
    await call("record_finance_refund", payload={"related_transaction_id": tx, "amount": 9}, idempotency_key="v1-refund")
    assert (await call("get_item", item_id=item_id))["quantity"] == 12
    await call("record_return", item_id=item_id, payload={"quantity": 1}, idempotency_key="v1-return")
    assert (await call("get_item", item_id=item_id))["quantity"] == 11
    assert (await call("get_account_balance", query={"account_id":account_id}))["current_balance"] == 40

    with urlopen(base + "/api/v1/intake/settings") as response:
        assert json.load(response)["ai_available"] is False
    try:
        urlopen(Request(base + "/api/v1/intake/not-required/analysis", data=b"{}", method="POST"))
    except HTTPError as err:
        assert err.code == 503 and json.load(err)["error"]["code"] == "ai_unavailable"
    else:
        raise AssertionError("AI analysis should be unavailable")

    print("SDK v1 PASS: isolated acquisition, expense recovery, annotation, links, retry, refund, return, disabled AI.")


asyncio.run(main())
