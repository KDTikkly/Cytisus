# AGENTS.md — Cytisus Codex Working Agreement

本文件约束所有 Codex 和工程代理行为。优先级仅次于明确的人类指令和 `PDM.md`。

## 1. 工作方式

1. 先阅读：
   - `PDM.md`
   - `DECISION_REGISTER.md`
   - 当前 Phase 的任务说明
2. 在修改前简述：
   - 目标；
   - 受影响模块；
   - 预期迁移/API；
   - 测试计划。
3. 使用功能分支。
4. 小步提交。
5. 测试通过后可推送功能分支并创建 Draft PR。
6. 永远不得自动合并 `main`。
7. 永远不得自动部署生产。
8. 不得绕过 CI、审批或安全检查。

## 2. 分支与 PR

分支建议：

```text
feat/<phase>-<short-name>
fix/<module>-<short-name>
chore/<scope>-<short-name>
```

Commit 使用清晰命令式描述。

Draft PR 必须包含：

- Summary；
- Scope；
- Architecture notes；
- Database migration；
- API changes；
- Ledger impact；
- Security/compliance impact；
- Tests；
- Screenshots；
- Known gaps；
- Rollback。

## 3. 禁止上传

不得提交：

- `.env`
- `.env.local`
- AWS key
- Provider secret
- Apple/Google private key
- Real webhook secret
- Real KYC data
- Identity document
- Bank statement
- Database dump
- Card PAN/CVV
- Crypto private key
- Mnemonic
- Production log
- Real customer email/phone/address
- Real API token

只允许：

- `.env.example`
- 合成 seed data
- Fake provider credentials
- Test certificates explicitly marked fake

## 4. 金融编码规则

- 金额、价格、数量、费率禁止 `float32`/`float64`。
- PostgreSQL 使用 `NUMERIC`。
- Go 使用经过批准的 Decimal 类型。
- 所有舍入显式且版本化。
- 所有资金变化必须通过 Ledger。
- 不允许直接 UPDATE balance。
- 不允许删除 Ledger entry。
- 更正使用 reversal + replacement。
- Provider 不得直接写余额。
- 所有金融命令需要 idempotency key。
- 所有外部事件需要 provider + external_event_id 唯一约束。
- 所有重要状态迁移产生 Audit。
- 账本事务与 Outbox 同事务提交。

## 5. 模块边界

模块不得：

- 直接写其他模块 schema；
- 读取其他模块内部表作为业务依赖；
- 引入循环依赖；
- 在 transport 层放业务规则；
- 在 provider adapter 中实现核心政策。

使用：

- Application service；
- Domain event；
- Public read projection；
- Stable contract。

## 6. Provider 规则

每个真实外部能力都要有：

- Interface；
- Local simulator；
- Capability；
- Health；
- Timeout；
- Idempotency；
- Error mapping；
- Contract tests。

生产不得回退到 Simulator。

模拟器必须明确标记 `SIMULATED`。

## 7. API

- OpenAPI 是外部契约。
- 修改 API 时更新 spec、生成客户端并做兼容性检查。
- 错误必须有 stable code。
- 高风险 POST 支持 Idempotency-Key。
- 不把内部数据库 ID 泄露为可预测序列。
- 时间使用 UTC RFC3339。
- 金额以字符串表达。
- 不在客户端判断最终权限。

## 8. 数据库

- 使用 pgx + sqlc。
- 核心模块禁止 ORM。
- SQL 必须可读。
- Migration 可回滚或说明不可逆。
- 约束尽量放入数据库。
- 并发策略显式。
- 集成测试使用真实 PostgreSQL/Testcontainers。
- 禁止 SQLite 代替金融集成测试。

## 9. 异步

- 使用 PostgreSQL Transactional Outbox。
- Worker 使用 `FOR UPDATE SKIP LOCKED`。
- Consumer 幂等。
- 重试指数退避。
- 永久失败进入 DLQ。
- Replay 不得重复资金影响。

## 10. 安全与隐私

- 默认最小权限。
- 敏感字段不可写日志。
- 错误不可泄露账户是否存在、制裁匹配详情或内部阈值。
- 所有 Admin 高风险动作强审计。
- 双人审批不可由同一人完成。
- 生产数据不得进入测试。
- 新依赖需要安全理由。
- 对外输入进行 schema validation。
- Webhook 进行签名和重放保护。
- iOS Keychain 保存适用本地凭证。
- Web 使用安全 Cookie，不使用 localStorage 保存长期敏感 token。

## 11. UI/UX

- 英文首发。
- 所有文案走 i18n key。
- 不硬编码用户文案。
- 显示价格状态：Real-time / Delayed / Indicative / Simulated / Stale。
- 显示资金状态。
- 不使用假“成功”状态。
- 不把 QR 作为首要动作。
- 不使用 VIP 文案。
- 关键错误说明原因和下一步。
- 支持 Reduce Motion/Transparency、High Contrast 和键盘操作。

## 12. 测试最低要求

任何金融功能必须有：

- Happy path；
- Duplicate event；
- Retry；
- Concurrent request；
- Invalid transition；
- Authorization failure；
- Provider timeout；
- Audit assertion；
- Ledger invariant；
- Idempotency assertion。

任何 UI 功能必须有：

- Loading；
- Empty；
- Error；
- Unauthorized；
- Disabled reason；
- Accessibility。

## 13. 不允许的捷径

- 不能用内存 Map 作为最终 Repository。
- 不能把业务状态只存在前端。
- 不能用 TODO 代替 MVP DoD 流程。
- 不能创建假 Provider Logo 或暗示已签约。
- 不能假设美国法律结论。
- 不能把 RWA token 宣称为真实证券。
- 不能实现隐藏点差。
- 不能把 Stablecoin 固定为 1 USD。
- 不能把模拟价格标为实时。
- 不能自动开放第三方提现。
- 不能在 Read-only mode 允许普通交易。

## 14. 完成定义

任务完成前必须运行适用命令：

```bash
make generate
make fmt
make lint
make test
make test-integration
make migrate-check
make openapi-check
make secret-scan
make build
```

若某命令不存在，先在 Phase 0 创建统一 Makefile。

## 15. 停止并请求人类决定的情况

- 需要确定真实持牌合作方；
- 需要法律结构；
- 需要真实费率；
- 需要生产 Secret；
- 需要合并 main；
- 需要生产部署；
- 需要真实客户数据；
- PDM 内存在冲突；
- 需要改变 Locked 决策；
- 需要新增高风险产品。
