# 物簿开发边界

- 本仓库为物簿 Life Ledger，基于 Timo348/Stockroom，保留母体结构与许可证。v1.0 独立事实源是当前架构基准，见 docs/V1.0_ARCHITECTURE.md。本轮交互基准为 docs/物簿 v1.0.1 开发任务书.md。
- Items 管理物品与库存，Finance 管理账户、流水及原始资金证据，Links 关联已存在的事实。各领域独立提交，AI 通过领域契约协助。
- 原生账单沿用锁定版本的 double-entry-generator 和现有微信、支付宝、银行模板。付款方式映射须经明确确认，银行资金归属以账单和账户身份为准。
- 相邻的朝汐 ZhaoXi 项目严格只读；不得修改源码、配置、Git、虚拟环境、缓存或数据，也不得在那里运行可能写文件的命令。
- 朝汐适配代码放在本仓库 tools/wubu_tool。需要测试真实 SDK 时使用 `python -B`，从只读 src 导入，禁止生成朝汐 __pycache__。安装或激活到朝汐环境需要用户另行改变只读要求。
- 同一领域写入涉及的多表变更必须使用同一事务。数量变化必须留下 InventoryEvent。资金校正记录变更前后值，保留原始证据。Agent 写入优先支持 Idempotency-Key。
- 修改 API 后同步 internal/web/static/openapi.json，并运行 scripts/sync-tool-schema.py。
- 运行 scripts/test.ps1；本地正式数据库 data/wubu.db、.tools、bin、.runtime 不入 Git。验证数据只放临时数据库。
