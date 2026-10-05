import io
import json
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from wubu_tool.client import WubuClient, WubuError

class ClientTests(unittest.TestCase):
    def test_read_escapes_ids_and_query(self):
        with patch("wubu_tool.client.urlopen", return_value=io.BytesIO(b'{"quantity":4}')) as call:
            result = WubuClient().call("get_item", item_id="a/b", query={"name":"无糖可乐", "hide_out":True})
            request = call.call_args.args[0]
            self.assertIn("/items/a%2Fb?", request.full_url)
            self.assertIn("hide_out=true", request.full_url)
            self.assertEqual(request.method, "GET")
            self.assertEqual(result["quantity"], 4)

    def test_write_requires_key_and_preserves_body(self):
        with self.assertRaises(ValueError): WubuClient().call("record_purchase_event", payload={})
        with patch("wubu_tool.client.urlopen", return_value=io.BytesIO(b'{"id":"purchase"}')) as call:
            WubuClient().call("record_purchase_event", payload={"total_amount":229}, idempotency_key="event-1")
            request = call.call_args.args[0]
            self.assertEqual(request.get_header("Idempotency-key"), "event-1")
            self.assertEqual(json.loads(request.data)["total_amount"], 229)

    def test_network_failure_does_not_retry_write(self):
        with patch("wubu_tool.client.urlopen", side_effect=URLError("timeout")) as call:
            with self.assertRaises(WubuError) as caught:
                WubuClient().call("consume_item", item_id="cola", payload={"quantity":1}, idempotency_key="consume")
            self.assertTrue(caught.exception.unknown_outcome)
            self.assertEqual(call.call_count, 1)

    def test_conflict_is_a_known_error(self):
        error = HTTPError("http://127.0.0.1", 409, "conflict", {}, io.BytesIO(b'{"error":{"code":"negative_quantity","message":"negative"}}'))
        with patch("wubu_tool.client.urlopen", side_effect=error):
            with self.assertRaises(WubuError) as caught:
                WubuClient().call("consume_item", item_id="cola", payload={"quantity":1}, idempotency_key="consume")
            self.assertEqual(caught.exception.code, "negative_quantity")
            self.assertFalse(caught.exception.unknown_outcome)

    def test_base_url_is_loopback_only(self):
        for url in ["https://example.com", "http://127.0.0.1/evil", "http://user:password@127.0.0.1", "http://10.0.0.1"]:
            with self.assertRaises(ValueError): WubuClient(url)

if __name__ == "__main__": unittest.main()
