# 参考节点示例 — 守规矩的 Agent

> F3（ASR 组件）在设计阶段被反事实推演砍掉。其残余价值之一——"守规矩节点"——降级为交付物中的参考示例（文档级），非产品组件。本文是那个示例。

本文展示一个 well-behaved agent 节点如何使用 OuSheng 进行协作。不是代码，是工作流——任何 AI Coding 工具（Claude Code / opencode / Codex / Pi / 人）读此文档 + `board-protocol.md` 即可复现。

---

## 场景

两个节点协作完成一个用户登录功能：

- **backend**（生产方）：提供 `/login` 接口
- **frontend**（消费方）：调用 `/login`，集成测试

看板在 `.ousheng/`，两人通过 `git push/pull` 同步。

---

## 周期 1：backend 提出契约

backend agent 启动 → hook 自动 `board read` → 看板空 → 开始工作。

backend 写第一张卡（reference 卡，proposed）：

```bash
cat > .ousheng/auth-api.yaml <<'EOF'
id: auth-api
owner: backend
task: 提供用户登录鉴权接口
status: proposed
version: 0
contract:
  kind: http
  breaking: false
  interface:
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
EOF
board write -file .ousheng/auth-api.yaml -expect 0 -dir .ousheng
# written auth-api version 1
git -C .ousheng push
```

**投影律检查**：只写了契约形状（method/path/behavior），没写实现代码。✓

---

## 周期 2：frontend 约定

frontend agent 启动 → hook 自动 `board read` → 看到 `auth-api` proposed。

frontend agree（proposed → agreed）：

```bash
git -C .ousheng pull
board read -id auth-api -dir .ousheng   # version 1
# 编辑：status: agreed, version: 1
board write -file .ousheng/auth-api.yaml -expect 1 -dir .ousheng
# written auth-api version 2
git -C .ousheng push
```

**状态机检查**：proposed → agreed 合法。✓

---

## 周期 3：backend 实现并上线

backend pull → 看到 agreed → 开始实现（plant 内部闭环，OuSheng 不介入）。

实现完成后，上线（agreed → live）：

```bash
git -C .ousheng pull
board read -id auth-api -dir .ousheng   # version 2
# 编辑：status: live, version: 2
board write -file .ousheng/auth-api.yaml -expect 2 -dir .ousheng
# written auth-api version 3
git -C .ousheng push
```

---

## 周期 4：frontend 集成验证

frontend pull → 看到 live → 跑集成探针。

探针通过后，frontend 写 verified + evidence（**C1 行为传感器**）：

```bash
git -C .ousheng pull
board read -id auth-api -dir .ousheng   # version 3
cat > .ousheng/auth-api.yaml <<'EOF'
id: auth-api
owner: backend
task: 提供用户登录鉴权接口
status: verified
version: 3
evidence:
  probe: "test/auth_integration: POST /login 200 + token 可用"
  passed_at_commit: abc123
  by: frontend
contract:
  kind: http
  breaking: false
  interface:
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
EOF
board write -file .ousheng/auth-api.yaml -expect 3 -dir .ousheng
# written auth-api version 4
```

**C1 检查**：
- evidence.probe 非空（真实探针描述）✓
- evidence.passed_at_commit 非空（可追溯）✓
- evidence.by = frontend（消费方验证，非生产方自证）✓

---

## 周期 5：收敛

```bash
board converge -dir .ousheng
# status: CONVERGED
```

全板 verified，无 proposed，无 blocker。收工。

---

## 异常路径 1：C2 破坏性变更

backend 想把 `/login` 改成 `/auth`（破坏性变更）：

```bash
# 编辑：path: /auth, breaking: true, version: 4
board write -file .ousheng/auth-api.yaml -expect 4 -dir .ousheng
# contract.breaking=true requires human_ack with non-empty approver
```

被拒。必须找人批：

```yaml
human_ack:
  approver: alice
  at_version: 4
```

```bash
board write -file .ousheng/auth-api.yaml -expect 4 -dir .ousheng
# written auth-api version 5
```

**C2 检查**：破坏性变更强制人工背书。✓

---

## 异常路径 2：CAS 冲突

backend 和 frontend 同时基于 v4 写卡：

```
backend: write -expect 4  →  成功，version → 5
frontend: write -expect 4  →  version conflict
```

frontend 必须重新 read 拿到 v5，在 v5 上重放变更，再 write -expect 5。

**禁止**：忽略 conflict 强写。CAS 是协作信任的基础。

---

## 异常路径 3：C3 加法自吸收

backend 给 `/login` 响应加一个可选字段 `refresh_token`（非破坏性）：

```yaml
contract:
  breaking: false  # 加法，非破坏性
  interface:
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
      response:
        "200":
          token: string
          expires_in: int
          refresh_token: string  # 新增可选字段
```

```bash
board write -file .ousheng/auth-api.yaml -expect 5 -dir .ousheng
# written auth-api version 6
```

无需 human_ack，无需 frontend 重新验证。frontend 的旧 evidence 仍有效——加法变更在控制死区内。

**C3 检查**：加法自吸收，不阻断收敛。✓

---

## 异常路径 4：依赖成环

两个卡互相依赖：

```
card A depends_on: [card B]
card B depends_on: [card A]
```

```bash
board converge -dir .ousheng
# status: STUCK
# cycle: [card A, card B, card A]
```

STUCK 是诚实信号——工具不会自动解环，人介入拆分契约或引入仲裁点。

---

## 守规矩清单

一个 well-behaved 节点：

- [x] 每周期开始先 `read_board`
- [x] 每周期结束 `write_board` 发布产出（若有变更）
- [x] 只投影 C（契约形状/状态/evidence），绝不投影 I（代码/推理/Memory）
- [x] `verified` 带真实 evidence，由消费方验证
- [x] `breaking=true` 带 human_ack
- [x] CAS 冲突时重新 read，不强写
- [x] 卡里不塞代码块/traceback/调试日志
- [x] 依赖成环时停手，等人介入

不守规矩的后果：硬闸门拒写（schema/CAS/状态机/breaking），或 converge 报 STUCK。软纪律管效率，硬闸门管底线。
