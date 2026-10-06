"""Run with a read-only Zhaoxi src on PYTHONPATH and python -B."""
import asyncio
import unittest
from unittest.mock import Mock
from pydantic import ValidationError
from zhaoxi.sdk import PermissionLevel
from wubu_tool.package import WubuInput, WubuTool, create_package
from wubu_tool.event_tools import RecordPurchaseTool, ValidatePurchaseTool, InventoryEventTool
from wubu_tool.client import WubuError

class SDKTests(unittest.TestCase):
    def test_package_and_permissions(self):
        package = create_package()
        tool = package.create_tools({})[0]
        self.assertTrue(package.capability_declaration().tool)
        self.assertEqual(tool.permission_for({"operation":"get_item"}), PermissionLevel.READ)
        self.assertEqual(tool.permission_for({"operation":"record_purchase_event"}), PermissionLevel.WRITE)
        self.assertFalse(tool.safe_to_replay({"operation":"consume_item"}))
        self.assertTrue(tool.safe_to_replay({"operation":"consume_item","idempotency_key":"1"}))
        schema=tool.schema()["function"]["parameters"]
        self.assertEqual(len(schema["allOf"]), 11)
        self.assertIn("PurchaseInput", schema["$defs"])

    def test_missing_key_is_rejected_before_network(self):
        with self.assertRaises(ValidationError): WubuInput(operation="consume_item",item_id="cola",payload={"quantity":1})

    def test_tool_passes_stable_key_to_client(self):
        client=Mock();client.call.return_value={"id":"ok"}
        tool=WubuTool(client)
        result=asyncio.run(tool.run({"operation":"consume_item","item_id":"cola","payload":{"quantity":1},"idempotency_key":"stable"}))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_args.kwargs["idempotency_key"], "stable")

    def test_purchase_schema_is_short_and_not_rest_dto(self):
        tools = create_package().create_tools({})
        self.assertEqual({tool.name for tool in tools}, {"wubu", "record_purchase", "validate_purchase", "wubu_inventory_event"})
        schema = tools[1].schema()["function"]["parameters"]
        self.assertEqual(set(schema["required"]), {"items", "idempotency_key"})
        self.assertNotIn("payload", schema["properties"])
        self.assertNotIn("operation", schema["properties"])
        item = schema["$defs"]["PurchaseItem"]
        self.assertEqual(set(item["required"]), {"name", "quantity", "unit"})
        self.assertNotIn('"type": "null"', __import__('json').dumps(schema))

    def test_single_tool_records_bottle_and_returns_confirmation(self):
        client=Mock()
        client.call.return_value={"purchase_id":"p1","expense_amount":5.5,"items":[{"item_id":"cola","name":"无糖可乐","spec":"2L","quantity":1,"quantity_added":1,"unit":"瓶","inventory_after":1}],"warnings":[]}
        args={"items":[{"name":"无糖可乐","spec":"2L","quantity":1,"unit":"瓶","price":5.5}],"idempotency_key":"purchase-1"}
        result=asyncio.run(RecordPurchaseTool(client).run(args))
        self.assertTrue(result.success)
        self.assertIn("当前 1瓶",result.content)
        self.assertEqual(client.call.call_count,1)
        self.assertEqual(client.call.call_args.args,("record_purchase",))
        self.assertEqual(client.call.call_args.kwargs["idempotency_key"],"purchase-1")
        self.assertEqual(client.call.call_args.kwargs["payload"]["items"][0]["spec"],"2L")
        self.assertNotIn("source",client.call.call_args.kwargs["payload"])

    def test_validation_is_read_only_and_inventory_uses_shared_resolver(self):
        client=Mock();client.call.return_value={"warnings":[]}
        preview=ValidatePurchaseTool(client)
        self.assertEqual(preview.permission,PermissionLevel.READ)
        self.assertTrue(preview.safe_to_replay({}))
        result=asyncio.run(preview.run({"items":[{"name":"无糖可乐","quantity":1,"unit":"瓶"}],"total":5.5}))
        self.assertTrue(result.success)
        self.assertNotIn("idempotency_key",client.call.call_args.kwargs)
        client.call.return_value={"item":{"quantity":2,"unit":"瓶"}}
        result=asyncio.run(InventoryEventTool(client).run({"action":"consume","name":"零度可乐大瓶","quantity":1,"idempotency_key":"drink-1"}))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_args.args,("consume_by_name",))
        self.assertEqual(client.call.call_args.kwargs["payload"]["source"],"agent")

    def test_candidate_errors_are_actionable_in_tool_result(self):
        detail={"code":"ITEM_AMBIGUOUS","message":"请指定规格","retryable":False,"suggested_action":"ask_user_to_select_candidate","candidates":[{"id":"2l","name":"无糖可乐","spec":"2L","unit":"瓶","current_quantity":1}]}
        client=Mock();client.call.side_effect=WubuError("请指定规格","ITEM_AMBIGUOUS",detail=detail)
        result=asyncio.run(InventoryEventTool(client).run({"action":"consume","name":"无糖可乐","quantity":1,"idempotency_key":"drink-1"}))
        self.assertFalse(result.success)
        self.assertEqual(result.data["error"],detail)
        self.assertEqual(result.metadata["idempotency_key"],"drink-1")

if __name__ == "__main__": unittest.main()
