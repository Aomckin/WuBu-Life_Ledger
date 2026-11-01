# 物簿 Life Ledger v0.3

东西有生命周期，钱有流向。基于 [Timo348/Stockroom](https://github.com/Timo348/Stockroom) 的 Go + SQLite 定向扩展，保留母体结构、库存、Intake、媒体、Docker 与 Apache-2.0 许可证。

## v0.3 Living Runway

首页现在展示未来已预留、安全可花与安全日均；可管理未来收支、固定周期和安全缓冲。生活约束页记录居住、押金与实际代垫；支出可区分日常 / 固定 / 安家 / 耐用品 / 特殊事件，库存支持历史消耗预测。

交付与计算口径见 [v0.3 交付记录](docs/V0.3_DELIVERY.md)，基线见 [v0.3 审计](docs/WUBU_V03_AUDIT.md)。本轮只在临时数据库验证，正式库及朝汐服务未迁移或重启。

## 启动

Windows 双击 `启动物簿.cmd`，或运行：

```powershell
.\scripts\start.ps1
```

打开 http://127.0.0.1:8026 。Ctrl+C 停止，正式数据在 `data/wubu.db`。默认只监听本机，不需要 AI 配置。Go >=1.25；可使用本机不入 Git 的 `.tools/go`。Linux/macOS 可运行 `go run ./cmd/stockroom`；Docker 配置保留，本次没有容器验收。

已有 v0.1/v0.1.1/v0.2 数据库会自动升到 schema 11，保留购买、库存、维护、别名和幂等记录。升级前请备份旧数据库；不要用正式库运行示例和测试。旧账户全部迁为 STORAGE，余额和资金历史不变。迁移细节见 [v0.2.1 交付记录](docs/V0.2.1_DELIVERY.md)。

## 已实现

- 账户：存储 / 消费一级分类，钱包、银行卡、现金、校园卡、信用卡、商家储值、其他形态；可绑定商家，初始余额生成事件，可设置默认付款账户、查询余额和核对真实余额。
- 资金流水：支出、收入、双向关联转账、关联退款、余额调整、受限赠送 BONUS。充值本金记转账，赠送不计普通收入；实际消费时再记支出。余额与事件同事务提交；转账不计消费或收入，退款冲减净支出。
- 购买：消耗品入库、耐用品独立实例、不跟库存的服务；购买、明细、库存历史、统一资金流水和账户扣款共用一个事务。改单和撤销保留资金补偿历史。
- 耐用品：独立仓库、品牌型号、序列号、购买信息、位置、保修和使用时长；在用、收纳、丢失、已售、退休，完整生命周期事件。出售同时入收入，退休保留历史。
- 维护：维护记录与费用扣款关联；可查询购入、累计维护、退款、总持有成本及净成本。
- 查询：上海时区今日、本周、本月、本年或自定义范围；分类、商家、商品和账户统计；商品购买次数、数量、最近价格和维护次数。
- 导入：支付宝、微信、Generic CSV 的只读预览和事务提交；订单号去重，无订单号使用稳定指纹。支付流水不自动生成商品库存。
- 页面：首页分开显示自由可用资金 / 消费储值，账户按分类分组，支持充值、分类编辑和账户流水详情；保留资金流水、耐用品、导入，保留原库存、购买、消费、维护与历史页面。
- Agent：现实动作高层工具，全部关键写入支持持久化 Idempotency-Key；同键同参数重放，变更参数返回 409，失败不占键。

金额 API 使用 CNY 元，最多两位小数，SQLite 存整数分。支出等 `amount` 为正的绝对值，`balance_delta` 表达方向。信用卡采用**净余额**，债务为负，转入还款增加余额，信用额度不计总资金。`liquid_balance` 是启用的存储账户资金，`consumption_balance` 是启用的消费储值，二者均排除信用卡和未指定账户；`available_balance` 兼容字段等于自由资金。未指定账户保留无法确认账户的旧账，不猜支付钱包，也不计真实总余额。

## API 与朝汐

- 契约：`GET /api/v1/openapi.json`
- 账户：`GET / POST /api/v1/accounts`，`PATCH /api/v1/money/settings`
- 资金：`POST /api/v1/money/income|expenses|transfers|refunds|adjustments|reconcile|top-ups|bonuses`
- 储值统计：`GET /api/v1/accounts/{id}/statistics`；账户列表可按 `account_class/merchant/active` 筛选。
- 查询：`GET /api/v1/money/balance|transactions|summary`，支持 `period=today|week|month|year` 或 `from/to`。
- 购买：`POST /api/v1/purchases` 或简化的 `/purchases/record`，新增 `account_id`；省略用默认账户。
- 耐用品：`GET /api/v1/durables`，`POST /api/v1/items/{id}/sell|retire|move|status`
- 成本：`GET /api/v1/items/{id}/total-cost`
- 导入：`POST /api/v1/money/import/preview`，确认后 `/money/import`

退款需关联原购买或支出，金额不得超过剩余未退金额；只有实际退回商品时才传 `return_items`。已退款的购买不再改单或撤销。收纳仍拥有数量 1，售出和退休终止生命周期，不能直接删除物品。

**朝汐 ZhaoXi 项目严格只读。** 适配与测试在本仓库 `tools/wubu_tool`，使用 `python -B` 从现有朝汐 src 导入 SDK，没有安装、激活或修改朝汐环境。见 [接入说明](docs/ZHAOXI_INTEGRATION.md)。

## 验证

```powershell
.\scripts\test.ps1
# 可选：只读验证真实朝汐 SDK 与临时 HTTP 服务
.\scripts\test.ps1 -ZhaoxiSource 'E:\桌面\改变世界的魔法\朝汐ZhaoXi\src'
```

测试使用临时 SQLite，覆盖旧版本迁移与重启、v0.2 / v0.2.1 任务书核心场景 A–I、并发幂等、转账守恒、退款与退库存、余额重建、跨领域失败回滚、持有成本、导入去重、HTTP/OpenAPI 和真实 SDK。浏览器验收脚本 `scripts/test-v02-ui.ps1` 对独立临时服务验证桌面与手机页面，需要已有 Edge 和 Playwright；可用 `-PlaywrightModule` 指定模块路径。

[v0.2.1 交付记录](docs/V0.2.1_DELIVERY.md) · [账户分类审计](docs/WUBU_V021_AUDIT.md) · [v0.2 开发审计](docs/WUBU_V02_AUDIT.md) · [v0.2 交付记录](docs/V0.2_DELIVERY.md) · [第三方声明](THIRD_PARTY_NOTICES.md) · [母体 README](docs/STOCKROOM_UPSTREAM_README.md) · [LICENSE](LICENSE)
