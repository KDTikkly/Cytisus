# Cytisus Decision Register

## Locked

| ID | 决策 |
|---|---|
| D-001 | 产品暂定名与工程代号为 Cytisus。 |
| D-002 | Web + 原生 iOS + 独立 Admin Web。 |
| D-003 | iOS 导航为 Home / Markets / Portfolio / Card / Account。 |
| D-004 | Web 使用固定工作区模板与有限调整。 |
| D-005 | 后端使用 Go 模块化单体，预留微服务拆分。 |
| D-006 | PostgreSQL + pgx + sqlc，金融核心禁止 ORM。 |
| D-007 | PostgreSQL Transactional Outbox + Go Worker，不在 MVP 引入 Kafka/NATS。 |
| D-008 | 本地 Simulator + 可替换 Production Adapter。 |
| D-009 | 身份为 Email + Passkey，Apple/Google 快捷登录。 |
| D-010 | 允许人工账户恢复，恢复后至少 72 小时资金安全冻结。 |
| D-011 | 疑似接管进入 Read-only Security Mode。 |
| D-012 | Read-only 默认禁交易，强验证和人工批准后可保护性卖出。 |
| D-013 | Paper 注册即有；Live 开通后用户端隐藏 Paper。 |
| D-014 | Paper → Live 仅迁移 Watchlist 与非金融偏好。 |
| D-015 | 美国证券仅 Common Stock 与 ETF 可交易。 |
| D-016 | Spot、long-only、cash-only、支持碎股。 |
| D-017 | 证券 Market/Limit、DAY/GTC。 |
| D-018 | 卖出所得可作为 provisional buying power 再投资，但结算前不可提现或转 Crypto。 |
| D-019 | 全市场延迟行情免费；合格 Live 用户可实时；Premium/Metal 流式。 |
| D-020 | Crypto 为纯托管 CEX，主流币白名单，USD 统一报价/结算。 |
| D-021 | 不提供币币对，跨资产显式经过 USD。 |
| D-022 | Crypto 使用多 Venue Smart Order Routing。 |
| D-023 | 所有价格改善归用户，平台只收透明费用。 |
| D-024 | 稳定币价格不固定为 1 USD。 |
| D-025 | Crypto 地址采用白名单与动态 24–72 小时冷静期。 |
| D-026 | ACH + USD Wire，同名账户优先，不支持银行卡直接投资入金。 |
| D-027 | 银行提现默认闭环；新增同名账户增强审核；第三方账户禁止。 |
| D-028 | 证券佣金为低固定费 + 极低比例费，会员分层。 |
| D-029 | Metal 免佣按周期累计成交额。 |
| D-030 | Metal 中途升级立即生效，额度按剩余周期比例发放。 |
| D-031 | Card 使用 USD 清算与动态资产支持 Spending Power。 |
| D-032 | Card 还款支持 CASH_ONLY / CASH_THEN_AUTO_SELL / MONTHLY_STATEMENT。 |
| D-033 | Auto-Sell 使用预授权资产列表与受保护限价。 |
| D-034 | 虚拟卡和本地 NFC 模拟完整实现。 |
| D-035 | Apple/Google Wallet 只做 Adapter 和真实状态入口。 |
| D-036 | 实体塑料卡/Metal 卡实现完整生命周期。 |
| D-037 | RWA 为许可型、完整份额、全额储备的产品模型。 |
| D-038 | RWA 首版单链 Base，本地兼容 Anvil，不使用跨链桥。 |
| D-039 | 默认平台 RWA Vault；增强审核后可转许可外部地址。 |
| D-040 | RWA 股息进入 Settled USD Cash；MVP 不支持 DRIP。 |
| D-041 | 报表完整，但不代报税。 |
| D-042 | 账户关闭而非承诺立即彻底删除；监管记录依法保留。 |
| D-043 | Push + Email + In-app Inbox。 |
| D-044 | en-US 正式交付；预留 zh-CN/zh-HK/ja-JP/ko-KR；底层 RTL。 |
| D-045 | AWS 美国区域 + Docker 容器化；本地 Docker Compose。 |
| D-046 | Codex 可推功能分支并创建 Draft PR，不可合并 main 或生产发布。 |
| D-047 | MVP 必须跑通 PDM Definition of Done 的端到端流程。 |

## Configurable

- 会员价格；
- 资产免年费阈值；
- Metal 月度免佣成交额；
- Standard/Premium/Metal 佣金数字；
- Crypto 交易费；
- Card FX markup；
- Card 额度；
- Asset haircut；
- Auto-Sell guardrail；
- AML 阈值；
- Cooling period 加权；
- 市场数据套餐；
- 支持国家；
- Asset/network capability；
- 报表留存；
- Notification 频率；
- Provider timeout/retry。

所有 Configurable 项必须：

- 有策略版本；
- 有生效时间；
- 有审计；
- 高风险修改 maker-checker；
- 订单/交易保存当时版本。

## External dependency

- 法律主体；
- 美国证券持牌结构；
- Broker/Clearing；
- Sponsor bank；
- ACH/Wire Provider；
- Custodian；
- Crypto licensure；
- Venue；
- Card issuer/program manager；
- RWA issuer/SPV/custodian；
- Market data license；
- KYC/AML/Sanctions vendor；
- Chain analytics；
- Tax forms；
- Data retention period；
- Production fee schedule；
- Supported jurisdictions。
