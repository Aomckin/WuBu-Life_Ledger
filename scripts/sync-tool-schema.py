"""Regenerate the Zhaoxi tool request schemas from the current REST contract."""
import json
from pathlib import Path
import runpy
import re

root = Path(__file__).resolve().parents[1]
routes = runpy.run_path(str(root / "tools/wubu_tool/wubu_tool/client.py"))["ROUTES"]
document = json.loads((root / "internal/web/static/openapi.json").read_text(encoding="utf-8"))
paths = {re.sub(r"\{[^}]+\}", "{}", path): value for path, value in document["paths"].items()}
inputs = {}
for operation, (method, route) in routes.items():
    if method == "GET":
        continue
    path = re.sub(r"\{[^}]+\}", "{}", "/api/v1" + route)
    body = paths[path][method.lower()].get("requestBody")
    if body is None:
        continue
    schema = body["content"]["application/json"]["schema"]
    if "$ref" in schema:
        inputs[operation] = schema["$ref"].rsplit("/", 1)[-1]

schemas = {}
pending = list(inputs.values())
while pending:
    name = pending.pop()
    if name in schemas:
        continue
    schemas[name] = document["components"]["schemas"][name]
    pending.extend(re.findall(r"#/components/schemas/([\w-]+)", json.dumps(schemas[name])))

output = json.dumps({"schemas": schemas, "inputs": inputs}, ensure_ascii=False, indent=2).replace(
    "#/components/schemas/", "#/$defs/"
)
(root / "tools/wubu_tool/wubu_tool/api_schema.json").write_text(output + "\n", encoding="utf-8")
