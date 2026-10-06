"""Regenerate the Zhaoxi tool's embedded request schemas from the API contract."""
import json
from pathlib import Path
root = Path(__file__).resolve().parents[1]
document = json.loads((root / "internal/web/static/openapi.json").read_text(encoding="utf-8"))
names = ["PurchaseInput", "PurchaseLine", "PurchaseAmendment", "PurchaseDeletion", "Category", "InventoryAction", "ConsumeInput", "InventoryAdjustment", "MaintenanceInput", "AliasInput", "DurablePatch"]
inputs = {"record_purchase_event":"PurchaseInput", "consume_item":"ConsumeInput", "adjust_inventory":"InventoryAdjustment", "open_item":"InventoryAction", "finish_item":"InventoryAction", "discard_item":"InventoryAction", "add_maintenance_event":"MaintenanceInput", "add_product_alias":"AliasInput", "update_durable_details":"DurablePatch"}
inputs.update({"amend_purchase_event":"PurchaseAmendment", "delete_purchase_event":"PurchaseDeletion"})
output = json.dumps({"schemas":{name:document["components"]["schemas"][name] for name in names}, "inputs":inputs}, ensure_ascii=False, indent=2).replace("#/components/schemas/", "#/$defs/")
(root / "tools/wubu_tool/wubu_tool/api_schema.json").write_text(output + "\n", encoding="utf-8")
