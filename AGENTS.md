# 物簿开发边界

- 本仓库为物簿 Life Ledger，基于 Timo348/Stockroom，保留母体结构与许可证。当前 v0.4 基准为 docs/物簿 v0.4 开发任务书｜Weekly Reconciliation & Semantic Backfill.md 及用户最新补充；旧 Financial Inbox 任务书已废止。采用 double-entry-generator 接入微信、支付宝专用原生账单入口，同时保留锁定上游版本的银行账单导入及模板入口；不做任意 CSV 自动识别，不恢复手机采集子项目。
- 相邻的朝汐 ZhaoXi 项目严格只读；不得修改源码、配置、Git、虚拟环境、缓存或数据，也不得在那里运行可能写文件的命令。
- 朝汐适配代码放在本仓库 tools/wubu_tool。需要测试真实 SDK 时使用 `python -B`，从只读 src 导入，禁止生成朝汐 __pycache__。安装或激活到朝汐环境需要用户另行改变只读要求。
- 一个现实事件的多表写入必须使用同一事务；数量变化必须留下 InventoryEvent；Agent 写入优先支持 Idempotency-Key。
- 修改 API 后同步 internal/web/static/openapi.json，并运行 scripts/sync-tool-schema.py。
- 运行 scripts/test.ps1；本地正式数据库 data/wubu.db、.tools、bin、.runtime 不入 Git。验证数据只放临时数据库。
