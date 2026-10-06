# 物簿朝汐工具包 v0.1.1

独立的朝汐 SDK v1 ToolPackage，开发和测试均在物簿目录。朝汐项目保持只读，因此本次不安装或激活工具。

入口：`wubu_tool.package:create_package`；包 ID：`wubu-tool`；工具：`record_purchase`、`validate_purchase`、`wubu_inventory_event`，以及兼容的高级 `wubu`。常用购买直接描述 name / spec / quantity / unit / price 和稳定幂等键，后端自动创建或复用商品；返回库存结果和需核对的警告。

详见物簿仓库 `docs/ZHAOXI_INTEGRATION.md` 的接口、金额单位、幂等与后续接入说明。默认服务地址为 `http://127.0.0.1:8026`。

无独立 HTTP 依赖，使用 Python 标准库；SDK / Pydantic 由朝汐环境提供。工具构造不访问网络；写入需要调用者提供幂等键。不能通过该工具启动或修改朝汐。

在物簿根目录测试标准库客户端：

```powershell
$env:PYTHONPATH = "$PWD\tools\wubu_tool"
python -B -m unittest discover -s tools\wubu_tool\tests -p test_client.py -v
```

若只读使用现有朝汐 SDK，可运行 `scripts/test.ps1 -ZhaoxiSource <朝汐 src 的绝对路径>`，全程 `-B` 防止缓存写回朝汐。
