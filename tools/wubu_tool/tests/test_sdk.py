"""Run with a read-only Zhaoxi src on PYTHONPATH and python -B."""
import asyncio
import unittest
from unittest.mock import Mock
from pydantic import ValidationError
from zhaoxi.sdk import PermissionLevel
from wubu_tool.package import WubuInput, WubuTool, create_package

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
        self.assertEqual(len(schema["allOf"]), 9)
        self.assertIn("PurchaseInput", schema["$defs"])

    def test_missing_key_is_rejected_before_network(self):
        with self.assertRaises(ValidationError): WubuInput(operation="consume_item",item_id="cola",payload={"quantity":1})

    def test_tool_passes_stable_key_to_client(self):
        client=Mock();client.call.return_value={"id":"ok"}
        tool=WubuTool(client)
        result=asyncio.run(tool.run({"operation":"consume_item","item_id":"cola","payload":{"quantity":1},"idempotency_key":"stable"}))
        self.assertTrue(result.success)
        self.assertEqual(client.call.call_args.kwargs["idempotency_key"], "stable")

if __name__ == "__main__": unittest.main()
