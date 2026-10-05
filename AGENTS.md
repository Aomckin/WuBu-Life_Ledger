# 物簿开发边界

- 本仓库为物簿 Life Ledger，基于 Timo348/Stockroom。按 docs 下 v0.1 任务书开发，保留母体结构与许可证。
- 相邻的朝汐 ZhaoXi 项目严格只读；不得修改源码、配置、Git、虚拟环境、缓存或数据，也不得在那里运行可能写文件的命令。
- 朝汐适配代码放在本仓库 tools/wubu_tool。需要测试真实 SDK 时使用 `python -B`，从只读 src 导入，禁止生成朝汐 __pycache__。安装或激活到朝汐环境需要用户另行改变只读要求。
- 一个现实事件的多表写入必须使用同一事务；数量变化必须留下 InventoryEvent；Agent 写入优先支持 Idempotency-Key。
- 修改 API 后同步 internal/web/static/openapi.json，并运行 scripts/sync-tool-schema.py。
- 运行 scripts/test.ps1；本地正式数据库 data/wubu.db、.tools、bin、.runtime 不入 Git。验证数据只放临时数据库。
