"""Run with a read-only Zhaoxi src on PYTHONPATH and python -B."""
import asyncio
import unittest
from unittest.mock import Mock
from pydantic import ValidationError
from zhaoxi.sdk import PermissionLevel
from wubu_tool.package import WubuInput, WubuTool, create_package
from wubu_tool.event_tools import InventoryEventTool
from wubu_tool.client import WubuError

class SDKTests(unittest.TestCase):
    def test_package_and_permissions(self):
        package = create_package()
        tool = package.create_tools({})[0]
        self.assertTrue(package.capability_declaration().tool)
        self.assertEqual(tool.permission_for({"operation":"get_item"}), PermissionLevel.READ)
        self.assertEqual(tool.permission_for({"operation":"record_acquisition"}), PermissionLevel.WRITE)
        self.assertFalse(tool.safe_to_replay({"operation":"consume_item"}))
        self.assertTrue(tool.safe_to_replay({"operation":"consume_item","idempotency_key":"1"}))
        schema=tool.schema()["function"]["parameters"]
        self.assertEqual(len(schema["allOf"]), len(__import__("json").loads(__import__("pathlib").Path(__import__("wubu_tool.package",fromlist=["x"]).__file__).with_name("api_schema.json").read_text(encoding="utf-8"))["inputs"]))
        self.assertIn("AcquisitionInput", schema["$defs"])

    def test_missing_key_is_rejected_before_network(self):
        with self.assertRaises(ValidationError): WubuInput(operation="consume_item",item_id="cola",payload={"quantity":1})

    def test_tool_passes_stable_key_to_client(self):
        client=Mock();client.call.return_value={"id":"ok"}
        tool=WubuTool(client)
        result=asyncio.run(tool.run({"operation":"consume_item","item_id":"cola","payload":{"quantity":1},"idempotency_key":"stable"}))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_args.kwargs["idempotency_key"], "stable")

    def test_acquisition_schema_contains_only_item_fields(self):
        tools = {tool.name: tool for tool in create_package().create_tools({})}
        self.assertIn("wubu_inventory_event", tools)
        self.assertNotIn("record_purchase", tools)
        schema = tools["wubu"].schema()["function"]["parameters"]
        line = schema["$defs"]["AcquisitionLine"]
        self.assertEqual(set(line["required"]), {"name", "quantity"})
        self.assertIn("unit", line["properties"])
        self.assertNotIn("unit_price", line["properties"])
        self.assertNotIn("account_id", schema["$defs"]["AcquisitionInput"]["properties"])

    def test_tool_records_only_acquisition(self):
        client = Mock()
        client.call.return_value = {"id": "p1", "items": [{"inventory_after": 1}]}
        line = {"name": "无糖可乐", "spec": "2L", "quantity": 1, "unit": "瓶"}
        result = asyncio.run(WubuTool(client).run({
            "operation": "record_acquisition", "payload": {"items": [line]},
            "idempotency_key": "acquisition-1",
        }))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_count, 1)
        self.assertEqual(client.call.call_args.args, ("record_acquisition",))
        self.assertEqual(client.call.call_args.kwargs["payload"], {"source": "agent", "items": [line]})

    def test_inventory_uses_shared_resolver(self):
        client = Mock()
        client.call.return_value = {"item": {"quantity": 2, "unit": "瓶"}}
        result = asyncio.run(InventoryEventTool(client).run({
            "action": "consume", "name": "零度可乐大瓶", "quantity": 1, "idempotency_key": "drink-1",
        }))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_args.args, ("consume_by_name",))
        self.assertEqual(client.call.call_args.kwargs["payload"]["source"], "agent")

    def test_candidate_errors_are_actionable_in_tool_result(self):
        detail={"code":"ITEM_AMBIGUOUS","message":"请指定规格","retryable":False,"suggested_action":"ask_user_to_select_candidate","candidates":[{"id":"2l","name":"无糖可乐","spec":"2L","unit":"瓶","current_quantity":1}]}
        client=Mock();client.call.side_effect=WubuError("请指定规格","ITEM_AMBIGUOUS",detail=detail)
        result=asyncio.run(InventoryEventTool(client).run({"action":"consume","name":"无糖可乐","quantity":1,"idempotency_key":"drink-1"}))
        self.assertFalse(result.success)
        self.assertEqual(result.data["error"],detail)
        self.assertEqual(result.metadata["idempotency_key"],"drink-1")

if __name__ == "__main__": unittest.main()
