# Cytisus Product Development Model（PDM）

**文档状态：** MVP 开发基线  
**产品代号：** Cytisus  
**主要交付端：** Web、原生 iOS、独立 Admin/Compliance Web Console  
**后端：** Go 模块化单体  
**默认语言：** en-US  
**最后更新：** 2026-07-19  
**用途：** 作为 Codex、工程团队、产品、设计、合规与测试共同使用的开发事实源。

> 本文是产品与工程规格，不构成法律、税务、证券、银行或监管意见。任何真实资金、真实证券、真实加密资产、真实银行卡或链上证券功能，上线前均须取得适用许可、持牌合作方支持及美国法律顾问确认。Codex 不得自行把模拟能力描述为已获许可或可生产使用。

---

## 0. 文档使用规则

1. 本文中的 **Locked** 决策不得由 Codex 擅自修改。
2. 标记为 **Configurable** 的数值必须进入版本化策略配置，不得硬编码。
3. 标记为 **External dependency** 的能力必须以 Provider Adapter、Capability Matrix 和明确的不可用状态实现。
4. 所有资产、资金和权限状态均以服务端为准；前端隐藏按钮不等于授权控制。
5. 所有金额、价格、数量、费率与汇率均禁止使用浮点数。
6. MVP 必须是可运行的金融模拟系统，而不是静态原型。
7. 默认本地环境不得要求真实第三方密钥、真实资金或真实身份数据。
8. 真实 Provider 与 Simulator 必须实现相同接口，但生产环境禁止启用模拟控制台。
9. 未在本文中明确批准的高风险功能，默认不进入 MVP。
10. 若实现与本文冲突，以本文、`AGENTS.md` 和 Decision Register 的优先级为准。

---

# 1. 产品概述

## 1.1 产品使命

Cytisus 是一个以美国市场资产查看和交易为第一身份、以支付和卡为第二核心层的全球金融产品。

使命是：

> 在严格遵守适用法律、制裁、客户尽调、资金来源和税务要求的前提下，让具有合法境外资金的全球用户更清晰地管理 USD、美国股票与 ETF、主流 Crypto、许可型 RWA 和日常支付。

Cytisus 不以规避资本管制、税务申报、制裁、CRS、KYC、AML 或当地法律为产品目标，也不得在文案、流程或后台中暗示此类用途。

## 1.2 MVP 目标用户

首要用户群：

- 持有合法境外资金的中国税务居民；
- 中国籍但在海外长期生活、工作或学习的全球流动用户；
- 拥有可验证本人境外银行账户或合规 Crypto 来源的自然人；
- 需要统一查看 USD、美国股票/ETF、Crypto、RWA 与卡消费的人群。

用户资格依据：

- 居住地；
- 税务居民地；
- 资金来源；
- 制裁与 PEP 检查；
- 持牌合作方覆盖范围；
- 产品适当性；
- 账户与设备风险。

**国籍本身不得作为唯一准入判断。**

## 1.3 MVP 用户范围

Locked：

- 仅自然人；
- 18 岁及以上；
- 不支持公司账户；
- 不支持联名账户；
- 不支持信托；
- 不支持机构账户；
- 不支持未成年人账户。

---

# 2. 产品原则

## 2.1 信任优先

- 所有费用在用户确认前可见；
- 所有状态有明确原因和下一步；
- 所有市场价格标记来源与时效；
- 不使用隐藏点差截留价格改善；
- 不把模拟价格、模拟发卡或模拟证券包装成真实服务；
- 不使用含糊的“风控失败”作为唯一解释；
- 不通过资产锁死制造留存。

## 2.2 美国本土金融产品表达

禁止：

- 功能宫格堆叠；
- 红点轰炸；
- 跑马灯；
- 首页签到、任务、拉新横幅；
- QR 作为主要交互；
- 悬浮客服球；
- 倒计时促销；
- VIP 1–10 等级游戏化；
- 霓虹、廉价金色或“暴富”视觉；
- 模糊错误；
- 隐藏费用；
- “稳赚”“财富自由”“独家机会”等承诺。

采用：

- Apple 式材质和动效；
- Robinhood 式移动端清晰度；
- IBKR 式 Web 信息密度；
- AmEx 式卡片品质感；
- 克制、透明、直接的英文文案。

## 2.3 资产不被困住

用户应具备三类退出路径：

1. 转至其他券商；
2. 将符合条件的完整证券份额铸造成许可型链上 RWA；
3. 卖出并提取 USD。

外部能力取决于 Provider、监管和用户资格，但系统必须保留清晰的数据模型和状态。

---

# 3. 产品范围

## 3.1 MVP 核心模块

- 身份与认证；
- 客户与设备；
- Progressive KYC；
- 合规案件；
- 双重记账 Ledger；
- Cash；
- ACH 与 USD Wire；
- 美国证券目录与行情；
- Paper Broker；
- 证券现货交易；
- Crypto 托管、充值、提现和现货交易；
- Smart Order Router；
- 虚拟卡与本地卡处理模拟；
- 动态资产支持的 Card Spending Power；
- 实体卡生命周期；
- RWA Vault、许可外部地址与 Base 模拟链；
- 会员与版本化定价；
- 报表与税务数据导出；
- 通知；
- Admin/Compliance Console；
- 账户恢复；
- Read-only Security Mode；
- 对账、审计和可观测性。

## 3.2 明确不进入 MVP

- 证券融资融券；
- 证券卖空；
- 期权；
- 期货；
- 永续合约；
- Crypto 杠杆；
- Crypto 借贷；
- Staking；
- 收益产品；
- 用户自行上币；
- Meme 币扩张策略；
- 公司、联名、信托或机构账户；
- DRIP；
- 自动报税；
- 第三方银行账户提现；
- 信用卡/借记卡直接向投资账户充值；
- RWA 跨链桥；
- 多链 RWA 同时发行；
- 自建生产级 Crypto 私钥托管；
- 自建底层 Passkey/OIDC 密码学；
- 真实 Apple Wallet/Google Wallet Tokenization；
- 自动合并 PR 或自动生产发布。

---

# 4. 产品端与信息架构

## 4.1 单一产品，多端分工

不拆成两个独立产品。

- **iOS：** 日常控制、查看、交易、卡和安全操作；
- **Web：** 专业研究、投资组合、订单与资金工作台；
- **Admin Web：** 独立企业身份、独立域名、独立权限与内部 API。

建议域名：

- `trade.example.com`
- `admin.example.com`
- `api.example.com`
- `internal-api` 仅私网

## 4.2 Web 工作区

固定模板 + 有限调整，不提供自由画布。

1. Trading
2. Portfolio
3. Research
4. Cash & Card

允许：

- 面板折叠；
- 有限尺寸调整；
- Tab 顺序；
- 默认工作区；
- 图表和显示偏好。

禁止：

- 任意拖拽形成不可维护布局；
- 用户脚本；
- 模糊的信息密度切换。

## 4.3 iOS 导航

Locked 底部导航：

1. Home
2. Markets
3. Portfolio
4. Card
5. Account

不设置独立 Trade Tab。交易从 Asset Detail、Position Detail 或 Watchlist 进入。

## 4.4 iOS 主要页面

### Home

必须回答：

- 当前净资产；
- 今日变化；
- 美国市场状态；
- Cash / Stocks / ETFs / Crypto 分布；
- Watchlist；
- Pending actions；
- Deposit / Withdraw / Convert 快捷入口；
- 安全或合规待办。

### Markets

- 全市场搜索；
- 股票、ETF、Crypto 分类；
- 行情状态标签；
- 可交易原因；
- 资产详情；
- Buy / Sell Liquid Glass 浮层。

### Portfolio

- Overview；
- Positions；
- Orders；
- Transfers；
- Activity；
- Buying Power；
- Settled Cash；
- Withdrawable Cash；
- Unrealized / Realized P&L；
- Settlement 状态。

### Card

- 虚拟卡；
- Freeze / Unfreeze；
- PIN；
- 动态可用额度；
- 额度解释；
- 还款模式；
- Auto-Sell Mandate；
- 消费、退款、FX、争议；
- 实体卡申请和物流。

### Account

- KYC；
- Tax profile；
- Bank accounts；
- Crypto addresses；
- RWA addresses；
- Membership；
- Security；
- Statements；
- Notifications；
- Support；
- Close account。

---

# 5. 设计系统

## 5.1 视觉方向

- iOS 使用原生 SwiftUI；
- Liquid Glass 风格，但不能牺牲可读性；
- Web 桌面端高密度；
- 卡模块具有独立但一致的高品质视觉；
- 日间和夜间环境氛围不改变语义色。

## 5.2 外观

用户控制：

- System；
- Light；
- Dark；
- Ambient Cycle On/Off。

Ambient phase：

- Dawn；
- Day；
- Dusk；
- Night。

仅改变：

- 背景；
- 光线；
- 材质；
- 微动效。

不得改变：

- 盈亏颜色含义；
- 风险级别；
- 成功/错误状态；
- 交易方向。

## 5.3 无障碍

必须支持：

- Reduce Motion；
- Reduce Transparency；
- High Contrast；
- Dynamic Type；
- VoiceOver；
- 键盘导航；
- 屏幕阅读器；
- Web focus ring；
- 文本扩张；
- RTL 底层布局。

---

# 6. 身份、认证与客户模型

## 6.1 登录方式

默认：

- Email + Passkey；
- Sign in with Apple；
- Sign in with Google。

手机号：

- 仅用于风险验证和通知；
- 不作为主要身份；
- 不作为唯一恢复路径。

## 6.2 Provider 架构

```text
IdentityProvider
├── LocalIdentitySimulator
├── PasskeyProviderAdapter
├── AppleOIDCAdapter
└── GoogleOIDCAdapter
```

平台保存：

- `provider`;
- `provider_subject`;
- email 属性；
- email 验证状态；
- 与内部 Customer 的映射。

不保存：

- 用户密码；
- Passkey 私钥；
- 生产 OIDC 密钥材料。

## 6.3 账户合并

- Apple Private Relay 邮箱不能仅凭邮箱自动合并；
- `provider_subject` 是稳定外部身份；
- 绑定新登录方式要求已登录会话 + 强验证；
- 身份冲突进入人工处理；
- 不允许客服直接修改 Provider Subject。

## 6.4 客户与能力

不要用单一 `account_status` 表达全部资格。使用能力矩阵：

```text
can_view_market
can_use_paper
can_fund_cash
can_trade_securities
can_trade_crypto
can_deposit_crypto
can_withdraw_crypto
can_use_card
can_mint_rwa
can_transfer_external_rwa
can_withdraw_bank
```

每个能力必须返回：

- allowed；
- reason_code；
- case_id；
- policy_version；
- next_action。

---

# 7. Progressive Onboarding

## 7.1 REGISTERED

完成：

- Email/Passkey 或 OIDC 注册；
- 基础设备注册；
- 接受条款。

能力：

- 浏览；
- 搜索；
- Watchlist；
- Paper Trading。

## 7.2 BASIC_KYC

完成：

- 身份证件；
- 活体；
- 地址；
- 税务居民地；
- 制裁/PEP；
- 基础客户风险。

可申请能力：

- Crypto 托管；
- Crypto 充值；
- Crypto 交易；
- 受限 Crypto 提现。

## 7.3 INVESTMENT_ELIGIBLE

增加：

- 投资适当性；
- 合法境外资金来源；
- 银行所有权验证；
- 税务与券商表单；
- Provider 覆盖；
- 证券风险披露。

能力：

- Live 证券交易；
- 银行资金；
- RWA；
- 完整 Card 资格评估。

## 7.4 ENHANCED_REVIEW

触发：

- 高金额；
- 高风险地区；
- 复杂资金来源；
- 账户恢复；
- 异常资金路径；
- 新同名提现账户；
- 高风险链上地址；
- 监管或 Provider 要求。

---

# 8. Paper 与 Live

## 8.1 Paper

注册后自动获得。

支持：

- 本地行情回放；
- Paper Broker；
- Market / Limit；
- DAY / GTC；
- 碎股；
- 部分成交；
- 取消；
- 拒绝；
- 过期；
- ACH / Wire 模拟；
- Crypto 模拟；
- Card 模拟；
- RWA 本地链模拟；
- Admin 模拟控制。

## 8.2 Live 开通后

- 普通用户界面隐藏 Paper；
- 不提供 Paper/Live 快速切换；
- Paper 数据保留为内部只读归档；
- 不迁移余额、持仓、P&L、订单、Card、会员进度、RWA 或资金记录。

允许迁移：

- Watchlist；
- Web 工作区；
- 图表偏好；
- 显示偏好；
- 通知偏好；
- 主题；
- Ambient；
- 无障碍；
- 语言。

不迁移 Auto-Sell mandate 或草稿。

---

# 9. 证券资产目录

## 9.1 市场范围

- USD 统一报价和结算；
- 美国市场；
- 全部美国上市 Instrument 可搜索和展示；
- MVP 只允许 Common Stock 与 ETF 交易。

其他资产在 MVP 为 View-only：

- ADR；
- REIT；
- Preferred；
- Closed-End Fund；
- ETN；
- Warrant；
- Unit；
- Right；
- 其他 Provider 不支持类型。

> 生产环境是否允许某一 Instrument 交易，必须由资产类型、Provider、司法辖区、用户资格和风险策略共同决定。

## 9.2 Capability Matrix

每个 Instrument 分开保存：

- listed；
- searchable；
- quote_enabled；
- paper_tradable；
- live_tradable；
- fractional_enabled；
- transfer_out_enabled；
- rwa_mint_enabled；
- user_eligible；
- disabled_reason。

## 9.3 资产目录来源

本地默认：

- Fixture symbol master；
- 可回放历史数据；
- 合成行情。

生产预留：

- 公共 Symbol Directory；
- Market Data Provider；
- Broker capability overlay；
- Corporate actions source。

---

# 10. 行情

## 10.1 行情等级

```text
REAL_TIME
DELAYED
INDICATIVE
SIMULATED
STALE
UNAVAILABLE
```

Locked：

- 全市场延迟行情免费；
- 完成投资资格后可开放实时行情；
- Premium / Metal 提供流式实时行情；
- 资格、行情授权、可交易性分别判断；
- Paper 价格不得伪装成真实实时行情；
- 下单确认显示报价类型与时间戳；
- 过期报价要求刷新；
- 行情中断不得静默使用旧价格。

## 10.2 风险估值行情

Card collateral、风险引擎、Auto-Sell 和对账使用独立的 Reference Price Policy：

- 多来源；
- 健康度；
- 时效；
- 异常值剔除；
- 交易暂停；
- 波动缓冲；
- 策略版本。

---

# 11. 证券交易

## 11.1 范围

- Spot；
- Long-only；
- Cash-only；
- Common Stock；
- ETF；
- Fractional 支持；
- 无杠杆；
- 无卖空；
- 无衍生品。

## 11.2 订单

MVP：

- Market；
- Limit；
- DAY；
- GTC。

IOC 可保留内部支持，但不作为首版用户入口，除非执行 Provider 需要。

## 11.3 市场时间

- 严格按真实支持时段；
- 不制造 24/7 股票流动性；
- 盘前/盘后仅在真实 Provider 支持且用户资格允许时开放；
- Paper 可模拟，但必须标记。

## 11.4 交收与 T+0 体验

用户卖出后：

- 成交所得可立即成为 `PROVISIONAL_BUYING_POWER`；
- 可用于重新购买符合策略的证券；
- 不可立即提现；
- 不可转换为 Crypto；
- 不可作为可提现 USD；
- 最终状态以 Provider/市场结算为准。

必须区分：

- Buying Power；
- Provisional Buying Power；
- Settled Cash；
- Withdrawable Cash；
- Card Spendable Cash；
- Frozen Cash。

## 11.5 转仓与外部 RWA

- 碎股可交易；
- 只有完整份额可转至其他券商；
- 只有完整、已结算、符合条件的份额可铸造 RWA；
- 碎股剩余必须留在平台或卖出。

---

# 12. Paper Broker 与 Broker Adapter

```text
BrokerAdapter
├── LocalBrokerAdapter
└── RealBrokerAdapter
```

Local Broker 必须达到 Paper Trading 等级：

- 市场日历；
- Market / Limit；
- DAY / GTC；
- 碎股；
- 部分成交；
- 拒单；
- 取消；
- 过期；
- 可重复 Replay；
- Live-paper 数据模式；
- 可配置延迟、滑点和错误；
- Corporate action fixture；
- 订单、持仓、现金和完整份额对账。

Broker 不得直接修改余额。流程：

```text
Broker event
→ Securities application service
→ Ledger posting
→ Position projection
→ Reconciliation
```

---

# 13. Crypto 资产与交易

## 13.1 资产白名单

本地模拟默认支持：

- BTC；
- ETH；
- SOL；
- XRP；
- BNB；
- DOGE；
- ADA；
- AVAX；
- LINK；
- LTC；
- USDC；
- USDT。

生产环境：

- 每个资产通过 Capability Matrix 独立控制；
- 不因本地存在而承诺生产可用；
- 需满足持牌合作方、托管、流动性、制裁、地区和监管条件。

## 13.2 报价与结算

Locked：

- USD 为唯一报价与结算中枢；
- 不提供 BTC/USDT 等币币交易对；
- 所有跨资产转换显式经过 USD。

例：

```text
USDT → USD → USDC
BTC → USD → ETH
SOL → USD → AAPL
```

每一腿拥有独立：

- Order；
- Fill；
- Fee；
- Tax lot；
- Ledger postings；
- Risk decision；
- Audit event。

## 13.3 Crypto 产品范围

- Spot；
- 24/7；
- 无杠杆；
- 无借贷；
- 无永续；
- 无期权；
- 无 staking；
- 无用户上币；
- 无隐藏跨链桥。

## 13.4 稳定币

USDC/USD 与 USDT/USD 是真实交易市场，不硬编码为 1 USD。

模拟器必须支持：

- 正常锚定；
- 小幅价差；
- 流动性下降；
- 短暂脱锚；
- 单侧暂停；
- 网络维护；
- 风险缓冲扩大。

---

# 14. Crypto Custody 与网络

## 14.1 托管模式

Locked：纯托管 CEX。

```text
CustodyProvider
├── LocalCustodySimulator
└── ProductionCustodyAdapter
```

- 平台生成充值地址；
- 平台维护内部账本；
- 平台广播提现；
- MVP 不把 WalletConnect/MetaMask 作为主流程；
- 真实私钥不得进入应用数据库、代码或普通环境变量；
- 生产签名使用持牌托管、MPC 或 HSM。

## 14.2 稳定币网络建议基线

USDC：

- Ethereum；
- Base；
- Solana；
- Arbitrum；
- Polygon。

USDT：

- Ethereum；
- Tron；
- Solana；
- TON；
- Avalanche。

原则：

- 仅官方 Native deployment；
- 不接受 wrapped、bridged 或同名假资产；
- 资产余额按网络细分，UI 可按资产聚合；
- 错误网络不得自动猜测入账。

其他主流币网络：

- BTC → Bitcoin；
- ETH → Ethereum；
- SOL → Solana；
- XRP → XRP Ledger；
- BNB → BNB Smart Chain；
- DOGE → Dogecoin；
- ADA → Cardano；
- AVAX → Avalanche C-Chain；
- LINK → Ethereum；
- LTC → Litecoin。

生产最终网络列表为 **External dependency + Configurable**。

## 14.3 Crypto → USD 审核

基础自动审查阈值：

- 单次转换 `< 25,000 USD`；
- 滚动 30 日累计转换 `< 25,000 USD`；
- 链上风险、账户风险和设备风险通过。

达到或超过任一阈值，或命中风险信号：

- Enhanced Review；
- USD 在审核与结算前不可用于证券、Card 或提现。

阈值必须版本化配置，不得硬编码在业务逻辑。

---

# 15. Smart Order Router

## 15.1 模式

Locked：多流动性来源智能路由。

```text
LiquidityVenueAdapter
├── LocalVenueA
├── LocalVenueB
├── LocalVenueC
└── ProductionVenueAdapter(s)
```

路由比较：

- 可执行价格；
- Venue fee；
- 预期滑点；
- 延迟；
- 健康度；
- 可成交数量；
- 失败概率；
- 库存/结算成本；
- 平台明确费用。

支持：

- 单 Venue；
- 拆单；
- 部分成交；
- Quote 过期；
- Venue 超时；
- 子订单取消；
- Stablecoin 脱锚；
- 成交偏离；
- Parent/Child 映射；
- 全链路审计。

## 15.2 价格改善

Locked：

- 全部价格改善归用户；
- 平台不得暗中截留；
- 平台收入来自明确交易费、会员、FX、Card、转仓或 RWA 服务费。

记录：

- Reference price；
- External average execution；
- Price improvement；
- Platform fee；
- Final customer consideration。

---

# 16. Cash 与银行

## 16.1 现金币种

MVP Cash Account 仅支持 USD。

Card 可在多币种商户消费，但清算到 USD。MVP 不建立 EUR、RMB、JPY 等现金钱包。

## 16.2 银行通道

Locked：

- ACH；
- USD Wire；
- 同名账户优先；
- 不支持信用卡/借记卡直接投资入金。

```text
BankRailProvider
├── LocalBankSimulator
├── AchProviderAdapter
└── WireProviderAdapter
```

## 16.3 ACH

状态：

```text
INITIATED
PROCESSING
SETTLED
RETURNED
CANCELED
```

支持：

- Instant verification；
- Micro-deposit；
- 入金；
- 提现；
- NSF；
- Account closed；
- Name mismatch；
- Return codes。

ACH 未最终结算前：

- 可按策略释放有限证券购买力；
- 不可买 Crypto；
- 不可链上提现；
- 不可银行转出；
- 不可作为真实 Card 现金额度。

## 16.4 USD Wire

状态：

```text
INSTRUCTIONS_ISSUED
FUNDS_DETECTED
OWNERSHIP_REVIEW
CREDITED
```

异常：

```text
NAME_MISMATCH
MISSING_REFERENCE
THIRD_PARTY_FUNDS
RETURN_REQUIRED
MANUAL_REVIEW
```

只有平台确认实际收到资金后，才增加 Settled Cash。

---

# 17. 银行提现与 AML

Locked：

1. 默认闭环提现；
2. 优先退回曾成功入金的本人同名账户；
3. 新增同名账户需要所有权验证、动态冷静期和增强审核；
4. 第三方账户在 MVP 永久禁止；
5. 只允许提取 `SETTLED` 且 `WITHDRAWABLE` 的 USD。

状态机：

```text
DRAFT
SECURITY_VERIFICATION
RISK_SCREENING
COOLING_OFF
APPROVED
SUBMITTED_TO_BANK
PROCESSING
SETTLED
```

异常：

```text
INFORMATION_REQUIRED
MANUAL_REVIEW
SANCTIONS_HOLD
NAME_MISMATCH
REJECTED
RETURNED
CANCELED
```

风险信号：

- Crypto → USD 后立即全额提现；
- 资金过桥；
- 多次略低于阈值；
- 新设备 + 新银行账户；
- 身份资料近期变更；
- 提现国家与居住/税务/资金来源不一致；
- 姓名不匹配；
- 来源无法解释；
- 制裁或高风险命中。

用户错误必须提供可解释原因和下一步，但不得泄露内部模型、规则阈值或规避方式。

---

# 18. Ledger

## 18.1 账本原则

- Double-entry；
- Immutable entries；
- 可逆转，不覆盖；
- 事件来源；
- Idempotency；
- Decimal；
- Versioned policy；
- Provider 不直接写余额；
- 所有资金影响由 Ledger Application Service 执行。

## 18.2 逻辑账户

至少区分：

- Crypto Account；
- Cash Account；
- Securities Account；
- Card subledger / receivable；
- Platform fee accounts；
- Provider clearing accounts；
- Suspense accounts；
- Frozen funds；
- RWA locked security accounts。

## 18.3 余额维度

- pending；
- held；
- settled；
- withdrawable；
- provisional buying power；
- card spendable；
- frozen；
- receivable。

## 18.4 精度

PostgreSQL 默认：

```sql
NUMERIC(38, 18)
```

业务显示：

- USD 默认显示到 2 位，但内部可高精度；
- Crypto 最多 18 位；
- 股票碎股按 Provider 能力；
- FX 保存原币金额、授权汇率、清算汇率与 USD；
- 舍入策略版本化。

## 18.5 绝对禁止

- `float32` / `float64` 表示金额；
- 直接 UPDATE balance；
- 删除已入账记录；
- Provider callback 自己加余额；
- 跨模块绕过 Ledger；
- 无幂等键的资金命令。

---

# 19. Card

## 19.1 MVP 边界

完整实现：

- 虚拟卡；
- 本地 NFC / Merchant Terminal 模拟；
- 授权；
- Hold；
- Capture；
- Partial capture；
- Reversal；
- Refund；
- Partial refund；
- Tips；
- Offline delayed；
- Duplicate；
- Decline；
- Dispute；
- 实体塑料卡状态机；
- Metal 卡状态机。

Adapter：

```text
CardProvider
├── LocalCardSimulator
└── RealCardIssuerAdapter
```

Apple Wallet / Google Wallet：

- 仅 Provider Adapter；
- UI 显示真实可用状态；
- 不伪造真实绑卡成功。

## 19.2 虚拟卡生命周期

```text
CREATE
ACTIVATE
SET_PIN
CHANGE_PIN
FREEZE
UNFREEZE
REPLACE
CLOSE
```

## 19.3 实体卡生命周期

```text
ELIGIBILITY_CHECK
APPLICATION_SUBMITTED
UNDER_REVIEW
APPROVED
MANUFACTURING
SHIPPED
DELIVERED
ACTIVATED
```

异常：

```text
REJECTED
ADDRESS_REVIEW
SHIPMENT_DELAYED
LOST
STOLEN
REPLACEMENT_REQUESTED
CANCELED
```

## 19.4 FX

USD settlement only。

记录：

- Merchant amount/currency；
- Authorization FX；
- Clearing FX；
- Authorized USD；
- Settled USD；
- FX markup；
- Pricing policy version。

会员 FX 策略为 Configurable。可采用示例：

- Standard：较高透明 markup；
- Premium：较低；
- Metal：月度额度内 0%，超额优惠费率。

数字不得作为 Locked 商业价格。

---

# 20. Dynamic Asset-Backed Spending Power

Locked：不使用简单固定 10%。

Rule Engine 考虑：

- Cash；
- Treasury ETF；
- Broad-market ETF；
- Large-cap stock；
- Normal stock；
- High-volatility stock；
- 分散度；
- 集中度；
- 用户信任等级；
- KYC；
- 历史；
- 市场状态；
- 绝对上限；
- Provider 上限；
- 会员仅影响产品权益，不直接替代风险审批。

排除：

- Crypto；
- Paper assets；
- Unsettled；
- Transferring；
- Frozen；
- Stale price；
- Halted assets。

系统必须向用户解释额度主要驱动因素，但不得泄露可被操纵的完整风险公式。

---

# 21. Card Repayment 与 Auto-Sell

## 21.1 还款模式

用户可配置：

```text
CASH_ONLY
CASH_THEN_AUTO_SELL
MONTHLY_STATEMENT
```

本地 MVP 全部模拟。真实生产开放取决于 Provider 与法律。

## 21.2 Auto-Sell Mandate

用户预授权：

- 可卖资产；
- 优先级；
- 最低保留数量；
- 是否允许卖碎股；
- 每日最大出售；
- 适用 Card repayment mode；
- 有效期；
- 策略版本。

执行：

1. 尝试第一个授权资产；
2. 无法成交时尝试下一个；
3. 使用 Protected Marketable Limit；
4. 支持部分成交；
5. 在 guardrail 内重新定价；
6. 不回退到裸市价；
7. 全部失败后冻结消费并要求用户处理。

不因价格下跌再次确认，但必须尊重用户预授权和保护范围。

---

# 22. RWA

## 22.1 法律与产品模型

MVP 产品模型：

> Permissioned, fully reserved, whole-share security token。

法律结构属于 **External dependency**。可能由券商、托管人、受监管 Tokenization Provider 或 SPV 实现。Codex 只能实现接口、状态机、模拟器和审计，不得宣称真实 Token 已代表法定证券权益。

## 22.2 网络

Locked：

- 首版单链；
- Base；
- EVM 通用接口；
- 本地使用 Anvil 或兼容本地链；
- 不做跨链桥；
- 未来迁移采用 Burn → Re-mint。

```text
RwaProvider
├── LocalRwaSimulator
└── BaseRwaProviderAdapter
```

## 22.3 Custody Mode

```text
PLATFORM_VAULT
EXTERNAL_PERMISSIONED_ADDRESS
```

默认：

- 铸造到平台托管 RWA Vault。

增强审核后：

- 可转至用户许可外部 Base 地址；
- 不要求钱包插件；
- 可手动添加地址；
- 必须证明控制权；
- 身份绑定；
- 制裁/链上风险；
- 冷静期；
- 只能许可地址间转移。

## 22.4 Mint

```text
Verify whole settled share
Verify investment eligibility
Validate destination
Lock underlying share
Create mint request
Mint RWA
Transfer to Vault or approved address
Reconcile supply and locked shares
```

## 22.5 Redeem

```text
Receive / identify token
Freeze token
Burn token
Confirm chain finality
Release underlying whole share
Reconcile
```

## 22.6 合约能力

本地模拟合约具备：

- Whitelist；
- Restricted transfer；
- Freeze；
- Pause；
- Forced redemption；
- Burn / Mint；
- Event log；
- Supply query。

## 22.7 资产权利

目标产品权利：

- 完整经济权益；
- 有限治理代理；
- 现金股息进入 Settled USD Cash；
- Corporate actions 自动反映；
- 底层资产与平台自有资产隔离；
- 不得未经披露出借或抵押；
- 投票权首版不提供直接链上投票；
- 预留 Pass-through voting。

真实权利由法律文件与 Provider 决定。

## 22.8 股息

Locked：

- 只发现金；
- 不提供 DRIP；
- 不自动购买 Token；
- 不自动发送稳定币。

流程：

```text
ANNOUNCED
RECORD_DATE_LOCKED
ENTITLEMENT_CALCULATED
PAYMENT_RECEIVED
WITHHOLDING_APPLIED
USD_CASH_CREDITED
RECONCILED
```

支持：

- 更正；
- 追回；
- 特别股息；
- 延迟；
- 拆股；
- 反向拆股；
- 并购；
- 退市；
- 现金收购；
- 记录日外部地址快照。

---

# 23. Membership 与 Pricing

## 23.1 等级

- Standard；
- Premium；
- Metal。

Paid subscription + asset-based annual fee waiver。

资产减免建议使用：

- 90-day average eligible assets；
- 具体价格与阈值 Configurable；
- 会员资格与风险额度分离。

## 23.2 Community Metal

可以授予：

- 安全研究；
- 高质量研究；
- 测试；
- 社区服务。

禁止作为：

- 拉新奖励；
- 交易量奖励；
- 普通推荐奖励。

Community Metal：

- 限时；
- 不可转让；
- 有来源和有效期；
- 通常不含实体 Metal 卡；
- 极少数人工授予可包含实体卡资格。

## 23.3 证券佣金

Locked 模式：

- 低固定费 + 极低成交额比例费；
- 会员分层；
- 全部价格改善归用户。

规则：

- 固定费按 Customer Parent Order 收一次；
- 拆 Venue/部分成交不重复固定费；
- 未成交不收费；
- 比例费按实际成交额；
- 买入费计入 Buying Power；
- 卖出费从所得扣除；
- 费用进入平台收入账本；
- 保存 `pricing_policy_version`。

费率数字为 Configurable。

## 23.4 Metal 免佣额度

Locked：

- 按每月累计已成交金额；
- 买卖都计入；
- 未成交不计；
- 边界订单可拆成免佣和收费部分；
- 监管费、转仓费、RWA 费不减免；
- 中途升级权益立即生效；
- 当期额度按剩余周期秒数比例发放；
- 升降级反复切换只补正向差额；
- 已产生费用不追溯退还。

---

# 24. Crypto 提现地址

状态：

```text
DRAFT
VERIFICATION_PENDING
COOLING_OFF
ACTIVE
SUSPENDED
REVOKED
```

动态冷静期基线：

- Base 24h；
- 新/不可信设备 +24h；
- 近期安全变更 +24h；
- 近期账户恢复 +48h；
- 自动上限 72h；
- 高风险审核暂停倒计时。

规则：

- 服务端计算；
- 显示原因；
- 地址白名单不绕过单笔 AML；
- 风险变化可暂停；
- 修改地址重新计算；
- 管理员不能无审计直接激活。

---

# 25. 账户恢复

允许人工恢复，但必须高强度。

流程：

```text
REQUESTED
EVIDENCE_REQUIRED
UNDER_REVIEW
APPROVED
SECURITY_HOLD
COMPLETED
```

要求：

- 身份证件重验；
- 活体；
- 历史账户资料比对；
- 已知设备与登录历史；
- 人工增强审核；
- 所有旧会话撤销；
- 新认证方式；
- 至少 72h 资金安全冻结；
- Card 默认冻结；
- 新银行/地址仍有独立冷静期；
- 提前解除需要双人审批；
- 不自动修改税务身份、法定姓名或资金来源。

---

# 26. Read-only Security Mode

## 26.1 触发

- 疑似账户接管；
- 重大设备异常；
- 认证冲突；
- 高风险恢复；
- 内部安全事件；
- 合规紧急措施。

## 26.2 能力

允许：

- 登录；
- 查看余额；
- 查看持仓；
- 查看历史；
- 查看安全案件；
- 下载既有报表。

禁止：

- 买入；
- 兑换；
- 提现；
- Card 消费；
- 新增银行/地址；
- RWA 铸造/赎回/转出；
- 修改身份、税务和安全资料；
- 创建 API Key。

## 26.3 保护性卖出

默认禁止交易。完成强验证并人工审核后允许：

- 仅卖出；
- 仅已有资产；
- Protected Limit；
- 可部分成交；
- 所得进入 `FROZEN_USD`；
- 不可提现、刷卡或重投；
- 单次/短时授权；
- 绑定 Case、审批人、最大数量和过期时间。

---

# 27. Admin / Compliance Console

## 27.1 隔离

- 独立域名；
- 独立企业身份；
- SSO + Passkey；
- 用户账户不得登录；
- 独立内部 API；
- 网络隔离；
- 敏感操作强验证。

## 27.2 角色

- SUPPORT；
- COMPLIANCE_ANALYST；
- RISK_ANALYST；
- OPERATIONS；
- FINANCE；
- ADMIN；
- AUDITOR。

## 27.3 高风险 Maker-Checker

需要双人审批：

- Ledger adjustment；
- Cooling period override；
- Force approval；
- Withdrawal unlock；
- Risk policy；
- Pricing policy；
- Provider policy；
- Production provider switch；
- Sensitive export；
- 账户恢复提前解冻。

例外：

- Emergency freeze 可单人；
- Unfreeze 必须双人。

## 27.4 Ledger 调整

管理员不能直接编辑余额。

必须：

```text
Create balanced adjustment proposal
→ Add reason and ticket
→ Attach evidence
→ Independent approval
→ Ledger posts immutable entries
→ Audit
```

## 27.5 Case Types

- BASIC_KYC_REVIEW；
- INVESTMENT_ELIGIBILITY_REVIEW；
- EDD；
- CRYPTO_TO_CASH_REVIEW；
- WITHDRAWAL_ADDRESS_REVIEW；
- TRANSACTION_MONITORING_ALERT；
- ACCOUNT_RECOVERY_REVIEW；
- BANK_WITHDRAWAL_REVIEW；
- SECURITY_TAKEOVER_REVIEW；
- RWA_ADDRESS_REVIEW。

## 27.6 Simulator Controls

本地/测试：

- 市场；
- Broker；
- Custody；
- Venues；
- Bank；
- Card；
- RWA；
- Webhook；
- 错误注入；
- Reconciliation mismatch；
- Time travel fixture。

生产必须完全禁用。

---

# 28. 通知

三级体系：

- Push；
- Email；
- In-app Inbox。

Push：

- 成交；
- Card；
- 提现状态；
- 安全事件。

Email：

- KYC；
- 银行；
- 提现审核；
- 月结单；
- 重大安全事件；
- 账户恢复。

In-app：

- 所有可追溯事件；
- 操作入口；
- 状态；
- Case 链接。

规则：

- 安全通知不可关闭；
- 法定披露不可关闭；
- 营销独立授权；
- 默认不与交易通知绑定；
- 使用 `notification_event_id` 去重；
- 支持用户渠道偏好；
- 保留发送状态和失败原因。

---

# 29. 报表、税务与数据

## 29.1 MVP 报表

- 月度账户报表；
- 年度账户汇总；
- 证券成交；
- Crypto 成交；
- 成本基础；
- 已实现盈亏；
- 股息；
- 预扣税；
- 银行资金流；
- 链上资金流；
- RWA Mint/Transfer/Redeem；
- Corporate actions；
- Card；
- FX；
- Fees；
- CSV；
- PDF。

## 29.2 税务边界

- 提供数据和适用税务文件能力；
- 不代报税；
- 不给跨国个性化税务结论；
- 税务文件取决于身份、税务居民地、账户结构和 Provider；
- 无资格或无数据时不得伪造官方税表。

## 29.3 账户关闭

模式：

- Close，而非承诺立即删除全部记录；
- 关闭前清空持仓、欠款、待结算、提现；
- 关闭全部能力；
- 撤销会话与凭证；
- 非必要数据删除或匿名化；
- 交易、AML、审计、税务和监管记录依法保留；
- 用户关闭前可导出；
- 不允许重新注册绕过旧风险记录；
- 保留政策版本化。

---

# 30. 国际化

首版正式交付：

- `en-US`

架构预留：

- `zh-CN`
- `zh-HK`
- `ja-JP`
- `ko-KR`

同时：

- RTL 底层支持；
- 首版不制作阿拉伯语；
- 所有文案使用 i18n key；
- 日期、金额、姓名、时区本地化；
- 支持文本扩张；
- Web 与 iOS 共享语义词汇表；
- 合规文案可按司法辖区覆盖；
- `en-US` 缺失构建失败；
- 其他缺失回退 `en-US`；
- 机器翻译不得直接进入生产。

---

# 31. 技术架构

## 31.1 总体

```text
Web: Next.js + TypeScript
iOS: SwiftUI
Admin Web: Next.js + TypeScript
Backend: Go
Database: PostgreSQL
Cache/ephemeral coordination: Redis
Database access: pgx + sqlc
Async: PostgreSQL Transactional Outbox + Go Worker
API: REST + OpenAPI
Local chain: Anvil-compatible EVM
Local runtime: Docker Compose
Cloud: AWS U.S. Region
```

## 31.2 模块化单体

领域：

- identity；
- customer；
- compliance；
- ledger；
- cash；
- banking；
- securities；
- marketdata；
- crypto；
- execution；
- custody；
- card；
- rwa；
- membership；
- pricing；
- reporting；
- notifications；
- admin；
- reconciliation；
- audit。

原则：

- 单一部署不等于无边界；
- 模块不得直接修改其他模块表；
- 跨模块命令通过公开 Application Service；
- 跨模块异步动作通过 Domain Event；
- 查询通过公开接口或 Read Projection；
- 共用 PostgreSQL，但独立 schema；
- 禁止随意跨 schema 联表；
- 未来可按吞吐、隔离或监管拆服务。

建议拆分顺序：

1. Market Data Worker；
2. Crypto Execution；
3. Ledger；
4. Compliance Case；
5. Card Processing。

---

# 32. Go 工程结构

```text
/apps
  /api
  /worker
  /migrate
  /simulator

/internal
  /identity
  /customer
  /compliance
  /ledger
  /cash
  /banking
  /securities
  /marketdata
  /crypto
  /execution
  /custody
  /card
  /rwa
  /membership
  /pricing
  /reporting
  /notifications
  /admin
  /reconciliation
  /audit

/pkg
  /money
  /events
  /idempotency
  /clock
  /auditlog
  /observability
  /providercontracts
  /testfixtures

/contracts
  /openapi
  /events
  /schemas

/apps/web
/apps/admin-web
/apps/ios
/deploy
/docs
```

模块内：

```text
/domain
/application
/repository
/provider
/transport
/projection
```

约束：

- Domain 不依赖 HTTP/DB/SDK；
- Application 定义事务边界；
- Repository 由 pgx/sqlc 实现；
- Provider 对接外部；
- Transport 暴露 REST/Webhook；
- Projection 服务前端查询；
- 依赖方向单向；
- 禁止循环依赖。

---

# 33. 数据库

Locked：

- PostgreSQL；
- `pgx`；
- `sqlc`；
- 显式 SQL；
- 核心金融模块禁止自动 ORM；
- 迁移使用可审计工具；
- Testcontainers 做集成测试。

金融写入：

- 事务；
- `SELECT ... FOR UPDATE` 或明确并发策略；
- 唯一幂等键；
- 版本号；
- Outbox；
- Database constraints；
- Server timestamps；
- NUMERIC；
- 不允许“先查再插”作为唯一幂等策略。

---

# 34. Transactional Outbox

首版不引入 Kafka/NATS。

流程：

```text
Business transaction
→ Domain data
→ Ledger postings
→ outbox_events
→ Commit
→ Worker claims with FOR UPDATE SKIP LOCKED
→ Consumer
→ Success / Retry / Dead Letter
```

要求：

- 多 Worker；
- 指数退避；
- 最大重试；
- DLQ；
- Admin 查看、重放、取消；
- 重放不能重复资金影响；
- Event version；
- Consumer idempotency；
- Future publisher 可转发至消息代理。

---

# 35. REST 与 OpenAPI

## 35.1 原则

- Web、iOS、Admin 使用 REST；
- OpenAPI 为客户端契约；
- 自动生成 TypeScript 和 Swift Client；
- 外部客户端不直接使用 gRPC；
- 内部未来拆服务可增加 gRPC；
- API 版本化；
- Idempotency-Key；
- Request ID；
- Error code；
- Capability reason；
- Pagination；
- Time cursor；
- Audit correlation。

## 35.2 主要 API 组

- `/v1/auth`
- `/v1/customers`
- `/v1/devices`
- `/v1/capabilities`
- `/v1/onboarding`
- `/v1/compliance`
- `/v1/market-data`
- `/v1/instruments`
- `/v1/orders`
- `/v1/positions`
- `/v1/cash`
- `/v1/banks`
- `/v1/transfers`
- `/v1/crypto`
- `/v1/addresses`
- `/v1/card`
- `/v1/rwa`
- `/v1/membership`
- `/v1/statements`
- `/v1/notifications`
- `/v1/security`
- `/internal/v1/admin`
- `/internal/v1/simulators`
- `/webhooks/{provider}`

---

# 36. Provider Adapter 列表

必须存在本地默认实现：

- IdentityProvider；
- BrokerAdapter；
- MarketDataProvider；
- LiquidityVenueAdapter；
- CustodyProvider；
- ChainAnalyticsProvider；
- BankRailProvider；
- CardProvider；
- RwaProvider；
- NotificationProvider；
- DocumentProvider；
- TaxDocumentProvider。

Provider 选择：

- 基于环境；
- Capability Matrix；
- 用户司法辖区；
- Provider 健康；
- 策略版本；
- Admin 审批。

生产 Provider 切换：

- 高风险；
- 双人审批；
- 审计；
- 可回滚；
- 不得由普通配置文件静默切换。

---

# 37. Security Baseline

## 37.1 加密

- TLS；
- 数据库静态加密；
- 对象存储静态加密；
- 备份加密；
- 高敏感字段应用层二次加密；
- AWS KMS；
- 密钥轮换；
- Secrets Manager；
- 禁止 Secret 写入代码、镜像和普通 `.env`。

## 37.2 分域

分离：

- 身份证件；
- 银行资料；
- 税务资料；
- 普通客户资料；
- 交易；
- 审计；
- 支持附件。

## 37.3 日志禁止

不得记录：

- 完整证件号；
- 完整银行账号；
- Card PAN；
- CVV；
- Crypto 私钥；
- 助记词；
- Access token；
- Refresh token；
- 完整链上签名材料；
- 原始 KYC 文档内容。

## 37.4 环境

- 生产数据不得复制到 local/test；
- Demo 数据必须合成；
- 管理员查看敏感数据需要理由、权限和审计；
- 真实 Crypto 私钥永不进入应用数据库；
- 真实 Provider Secret 仅通过 Secrets Manager / GitHub Environment。

## 37.5 会话与设备

- 新设备风险评估；
- 高风险操作 re-auth；
- 会话撤销；
- Device trust；
- Recovery hold；
- Read-only mode；
- 登录速率限制；
- Webhook 签名验证；
- CSRF/CORS/Content Security Policy；
- SAST/依赖/Secret scanning。

---

# 38. 部署

## 38.1 环境

- local；
- test；
- staging；
- production。

## 38.2 本地

Docker Compose 一键启动：

- PostgreSQL；
- Redis；
- API；
- Worker；
- Web；
- Admin；
- Simulators；
- Local chain；
- Mail catcher；
- Push mock；
- Object storage mock。

不得要求付费服务或真实密钥。

## 38.3 AWS 美国区域

建议：

```text
CloudFront
→ Web / Admin Web
→ ALB
→ Go API / Worker
→ PostgreSQL
→ Redis
→ Object Storage
→ KMS / Secrets
→ Logs / Metrics / Traces / Alerts
```

要求：

- 用户端与 Admin 隔离；
- 内部 API 私网；
- DB/Redis 不暴露公网；
- 生产禁用 Simulator；
- 容器化；
- 可移植；
- 变更可审计；
- 回滚；
- 备份与恢复演练；
- Provider health alerts。

---

# 39. Observability

必须具备：

- Structured logs；
- Metrics；
- Distributed traces；
- Request ID；
- Customer-safe correlation ID；
- Provider latency；
- Queue lag；
- Outbox backlog；
- Ledger imbalance alarm；
- Reconciliation mismatch；
- Withdrawal review queue；
- Card decline rate；
- Order reject rate；
- Quote staleness；
- Security mode activation；
- Admin high-risk action；
- Alert routing。

不得把敏感客户数据作为 metric label。

---

# 40. Reconciliation

至少包括：

- Ledger debit/credit balance；
- Cash vs bank provider；
- Securities positions vs broker；
- Orders/fills vs broker；
- Whole shares vs transferable shares；
- RWA supply vs locked securities；
- Crypto internal balances vs custody；
- Network deposits/withdrawals；
- Card holds/captures/refunds vs issuer；
- Platform fees；
- Membership allowance；
- Corporate actions；
- Notification delivery。

对账：

- 自动；
- 可重跑；
- 有差异状态；
- 有 Case；
- 不自动掩盖；
- 人工调整走 maker-checker。

---

# 41. Testing

## 41.1 层级

- Domain unit tests；
- Application tests；
- SQL repository integration tests；
- Provider contract tests；
- API contract tests；
- Outbox idempotency tests；
- Ledger invariant tests；
- State machine tests；
- End-to-end tests；
- Web accessibility tests；
- iOS UI tests；
- Admin permission tests；
- Security tests；
- Migration tests；
- Reconciliation tests。

## 41.2 必测异常

- 重复 Webhook；
- 并发提现；
- 部分成交；
- Provider 超时；
- Quote 过期；
- Stablecoin 脱锚；
- ACH return；
- Wire name mismatch；
- Card partial capture；
- Duplicate Card authorization；
- RWA mint 后链失败；
- Burn 成功但释放失败；
- Outbox 重放；
- Worker 崩溃；
- Account recovery；
- Read-only protective sell；
- Admin 越权；
- 双人审批冲突；
- 生产环境启用模拟器；
- Float contamination；
- Ledger imbalance。

---

# 42. MVP Definition of Done

以下流程必须端到端跑通，不接受仅静态页面：

1. 注册 / Passkey 登录；
2. Paper Trading；
3. KYC 与投资资格模拟；
4. Live Account 开通；
5. ACH / Wire 模拟入金；
6. 股票、ETF、Crypto 下单与成交；
7. 双重账本入账及结算；
8. Crypto 充值、转换与提现审核；
9. Card 授权、清算、退款与 Auto-Sell；
10. RWA 铸造至 Vault；
11. 外部许可地址；
12. RWA 转移与赎回；
13. 股息进入 USD Cash；
14. 银行闭环提现；
15. 报表导出；
16. Admin 案件审核与双人审批；
17. 账户恢复；
18. Read-only Security Mode；
19. 保护性卖出；
20. 对账与差异处理。

每条流程必须有：

- 状态机；
- Ledger；
- Audit；
- 错误路径；
- 自动测试；
- Web；
- 关键 iOS 路径；
- Admin 操作。

---

# 43. 分阶段开发计划

## Phase 0：Repo 与工程基线

交付：

- Monorepo；
- Go workspace；
- Next.js Web/Admin；
- SwiftUI skeleton；
- Docker Compose；
- PostgreSQL/Redis；
- sqlc；
- OpenAPI；
- CI；
- Secret scan；
- AGENTS.md；
- Branch protection 文档；
- Demo identity。

验收：

- 一条命令启动；
- CI 全绿；
- 无真实 Secret；
- API health；
- Web/Admin/iOS build。

## Phase 1：Identity、Customer、Capability、Audit

交付：

- LocalIdentitySimulator；
- Customer/LoginIdentity/DeviceSession；
- Capability engine；
- Audit；
- Session；
- Passkey/OIDC Adapter contract；
- i18n base；
- Admin RBAC skeleton。

## Phase 2：Ledger、Cash、Outbox

交付：

- Accounts；
- Entries；
- Posting templates；
- Holds；
- Balances；
- Outbox Worker；
- Idempotency；
- DLQ；
- Reconciliation core；
- Admin read-only ledger viewer。

验收：

- 所有资金测试平衡；
- 并发和重放不重复；
- 无 float。

## Phase 3：Market Data 与 Paper Securities

交付：

- Instrument master；
- Capability Matrix；
- Quote status；
- Market calendar；
- Paper Broker；
- Orders/Fills/Positions；
- Web Trading/Portfolio/Research；
- iOS Markets/Portfolio 基础。

## Phase 4：Onboarding、Compliance、Banking

交付：

- Progressive KYC Simulator；
- Case system；
- ACH/Wire simulator；
- Ownership；
- Provisional buying power；
- Closed-loop withdrawal；
- Cooling；
- Maker-checker。

## Phase 5：Crypto

交付：

- Asset/network matrix；
- Custody simulator；
- Deposit/withdrawal；
- Address whitelist；
- Chain risk mock；
- Crypto → USD review；
- Venue simulator；
- SOR；
- Stablecoin depeg cases；
- USD-only pairs。

## Phase 6：Card

交付：

- Virtual card；
- Merchant terminal simulator；
- Auth/hold/capture/refund；
- FX；
- Dynamic spending power；
- Repayment modes；
- Auto-Sell；
- Physical card lifecycle；
- Card UI；
- Notifications。

## Phase 7：RWA

交付：

- Anvil/local Base；
- Permissioned token；
- Vault；
- External address proof mock；
- Mint/Burn/Redeem；
- Share lock；
- Supply reconciliation；
- Corporate actions；
- Cash dividend。

## Phase 8：Membership、Pricing、Reporting

交付：

- Membership；
- Commission engine；
- Metal allowance；
- Proration；
- FX tiers；
- Statements；
- CSV/PDF；
- Cost basis；
- Tax data；
- Account closure/export。

## Phase 9：Security Hardening

交付：

- Account recovery；
- Read-only mode；
- Protective sell；
- KMS/Secrets integration；
- Environment isolation；
- Admin sensitive access；
- Security notifications；
- Pen-test readiness；
- Runbooks。

## Phase 10：Production Adapter Readiness

不要求启用真实 Provider，但必须：

- Provider contract test suite；
- Capability fallback；
- Health checks；
- Webhook security；
- Staging configuration；
- Provider switch maker-checker；
- Legal/partner dependency checklist；
- No false production claims。

---

# 44. GitHub 与 Codex 工作流

Locked：

```text
Codex local workspace
→ feature branch
→ format/lint/test/migration checks
→ commit
→ push
→ Draft PR
→ CI
→ human review
→ merge
```

Codex 可以：

- 创建功能分支；
- 修改代码；
- 提交；
- 推送；
- 创建 Draft PR；
- 更新 Draft PR；
- 读取 CI；
- 修复 CI。

Codex 不可以：

- 直接推送 main；
- 自动合并；
- 发布生产；
- 修改组织级安全策略；
- 上传 Secret；
- 上传真实用户/KYC/银行/Card/Crypto 数据；
- 绕过人工审批；
- 将模拟功能标记为真实可用。

---

# 45. 建议代码质量门槛

每个 PR：

- 小范围；
- 一个主要目标；
- 有测试；
- 有迁移影响；
- 有 OpenAPI 影响；
- 有安全影响；
- 有回滚说明；
- 有截图或录屏（UI）；
- 有状态机说明（金融流程）；
- 有 Ledger postings 示例（资金流程）；
- 有审计事件；
- 无未解释 TODO；
- 无禁用测试。

核心命令建议：

```bash
make bootstrap
make generate
make fmt
make lint
make test
make test-integration
make test-e2e
make migrate-check
make openapi-check
make secret-scan
make build
make compose-up
```

---

# 46. 核心实体建议

以下为最小核心集合，不是最终数据库表名：

- Customer
- LoginIdentity
- Device
- Session
- CapabilityDecision
- ComplianceProfile
- ComplianceCase
- CaseAction
- LinkedBankAccount
- BankTransfer
- Instrument
- InstrumentCapability
- MarketQuote
- CustomerOrder
- ChildOrder
- Fill
- Position
- SettlementLot
- LedgerAccount
- LedgerTransaction
- LedgerEntry
- BalanceProjection
- CryptoAsset
- CryptoNetwork
- CryptoAddress
- CryptoTransfer
- Venue
- VenueQuote
- ExecutionPlan
- Card
- CardTransaction
- CardHold
- CardDispute
- AutoSellMandate
- RwaAsset
- RwaAddress
- RwaMintRequest
- RwaRedemption
- CorporateAction
- DividendEntitlement
- Membership
- PricingPolicy
- AllowancePeriod
- Statement
- Notification
- OutboxEvent
- ProviderEvent
- AuditEvent
- ReconciliationRun
- ReconciliationBreak
- ApprovalRequest

---

# 47. 通用状态建模规则

任何高风险状态机必须：

- 明确 terminal 状态；
- 明确可逆与不可逆；
- 验证合法迁移；
- 记录 actor；
- 记录 reason；
- 记录 policy_version；
- 记录 occurred_at；
- 记录 correlation_id；
- 通过 Application Service 执行；
- 产生 Audit；
- 必要时产生 Outbox；
- 不允许前端直接设置 status。

---

# 48. 错误与文案

API 错误结构应包含：

```json
{
  "code": "BANK_ACCOUNT_COOLING_OFF",
  "message": "This bank account is still in its security waiting period.",
  "next_action": "Try again after the waiting period ends.",
  "retryable": false,
  "correlation_id": "..."
}
```

不得：

- 泄露内部阈值；
- 暴露制裁匹配细节；
- 暴露其他账户是否存在；
- 只返回 `Something went wrong`；
- 把系统错误描述成用户违规；
- 用营销语气处理资金失败。

---

# 49. 外部依赖与未决法律事项

以下不能由 Codex 决定：

1. 美国主体结构；
2. Broker/clearing partner；
3. Custody partner；
4. Crypto licenses；
5. Money transmission；
6. Card issuer/program manager；
7. ACH/Wire sponsor bank；
8. RWA 法律包装；
9. Token holder 的实际证券权利；
10. 各司法辖区准入；
11. 市场数据授权；
12. 税表与预扣义务；
13. 数据留存年限；
14. SAR/AML 操作程序；
15. Sanctions screening vendor；
16. KYC vendor；
17. Chain analytics vendor；
18. 真实定价与收费；
19. 真实 Card credit/receivable 模式；
20. 实体卡材质、物流与地区。

系统必须允许这些事项通过 Provider 和版本化 Policy 注入。

---

# 50. Codex 首轮执行边界

Codex 第一轮不得尝试一次性实现全部功能。必须：

1. 阅读本 PDM；
2. 阅读 `AGENTS.md`；
3. 读取 `DECISION_REGISTER.md`；
4. 先生成 ADR 和 Repo plan；
5. 创建 Phase 0；
6. CI 全绿后创建 Draft PR；
7. 再进入 Phase 1；
8. 每个 Phase 单独 Draft PR 或小批次 PR；
9. 每次变更报告：
   - 做了什么；
   - 没做什么；
   - 测试；
   - 风险；
   - 下一步；
   - 是否修改数据模型/API。

---

# 51. 最终产品成功标准

MVP 成功不是“功能多”，而是：

- 用户可以清楚理解每一美元和每一资产处于什么状态；
- 所有资金变化可由双重账本解释；
- 模拟器足够真实地覆盖异常；
- Provider 可替换；
- 风险与合规可解释、可审计；
- Web 具备专业工作台；
- iOS 具备日常控制体验；
- Admin 能完成真实案件流程；
- GitHub 工作流安全；
- Codex 可分阶段开发；
- 项目不会因为真实合作方尚未确定而停摆；
- 项目也不会把模拟能力误称为真实金融服务。

---

# 52. Glossary

- **Buying Power**：可用于买入的额度。
- **Provisional Buying Power**：底层未最终结算但策略允许再投资的额度。
- **Settled Cash**：已结算 USD。
- **Withdrawable Cash**：通过全部限制后可转出的 USD。
- **Frozen USD**：可见但不可使用的 USD。
- **Parent Order**：客户提交的母订单。
- **Child Order**：路由到外部 Venue 的子订单。
- **Price Improvement**：相对参考价格的更优执行。
- **RWA Vault**：平台托管的许可型 RWA 地址。
- **Permissioned Address**：完成身份与风险验证的外部地址。
- **Maker-Checker**：创建者与审批者分离。
- **Transactional Outbox**：业务数据和待发送事件同事务提交。
- **Capability Matrix**：按用户、资产、地区、Provider 和风险决定能力。
- **Paper**：明确标记的模拟环境。
- **Live**：接入真实 Provider 后的真实账户环境。
