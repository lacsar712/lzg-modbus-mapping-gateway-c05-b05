# 工业 Modbus 点位监控台（Modbus Mapping Gateway）

Mock PLC + Go 映射网关 + Vue3 监控前端，Docker Compose 一键启动。

## How to Run

```bash
cd projects/05-modbus-mapping-gateway
docker compose up --build
```

启动后访问：

| 服务 | 地址 |
|------|------|
| Frontend | http://localhost:3175 |
| Backend API | http://localhost:8175 |
| Mock Modbus PLC | localhost:15025 → 容器内 `5020` |

禁止端口：3264 / 8264 / 33264（本项目未使用）。

## 账号

| 用户 | 密码 | 权限 |
|------|------|------|
| engineer | mod123456 | 可读 / 写点 / reload |
| observer | obs123456 | 只读 |

## 架构

- **mock-plc**：纯 Python Modbus TCP Server（FC 0x03/0x06/0x10），预置 holding registers
- **backend**：Go + Gin，Hexagonal 分层；YAML DSL；自研类型编解码；寄存器区间合并 snapshot
- **frontend**：Vue 3 + Vite + Element Plus + nginx `/api` 反代

## API

- `POST /api/auth/login`
- `GET  /api/health`
- `POST /api/reload`（body 可选 `{ "yaml": "..." }`；失败保留旧配置）
- `GET  /api/mapping`
- `GET  /api/devices`（可选 `?sort=lastFailure|avgDuration`，响应含每台设备的 `diag` 诊断摘要）
- `GET  /api/devices/{id}/points`
- `GET  /api/devices/{id}/points/{name}`
- `PUT  /api/devices/{id}/points/{name}` body `{ "value": <number> }`
- `GET  /api/devices/{id}/snapshot`（手动 snapshot，同时写入采样环，`source=manual`）
- `GET  /api/diagnostics`（采样环用量 + 每台设备采样状态与统计）
- `GET  /api/diagnostics/samples?deviceId=&limit=`（采样环记录，最新在前）
- `POST /api/diagnostics/{id}/start` body `{ "intervalMs": 1000 }`（仅 engineer；非 idle 返回 409）
- `POST /api/diagnostics/{id}/stop`（仅 engineer；idle 返回 409）

## 设备诊断：采样状态机与持久化采样环

每台设备一个采样状态机：`idle → running → stopping → idle`。

- 仅 `engineer` 可 `start`/`stop`；`observer` 只能读矩阵与采样记录。
- `start` 仅在 `idle` 时成功；设备已在 `running`/`stopping` 时再次 `start` 返回 **409 冲突**，且不会开出第二个采样循环（状态机保证单循环）。
- `stop` 把 `running` 转为 `stopping` 并通知循环退出；在途的那次采样结果（无论成败）**丢弃不入环**，循环退出后回到 `idle`。`stopping` 期间不会再有任何采样器记录入环。
- 手动 `snapshot` 与采样器循环都会写入同一个**环形缓冲**（容量 500 条，FIFO 驱逐最旧记录）。记录字段：`seq`、`deviceId`、`source`（`manual`/`sampler`）、`startedAt`、`durationMs`、`success`、`badPoints`（quality 非 good 的点位）、`error`（Modbus 错误）。
- 环是**只追加**的：成功采样不会删除环里旧的失败记录，只有容量满时才按 FIFO 挤掉最旧记录。

### 持久化与重启恢复策略

采样环与各设备采样状态每次变更即原子写入 `DIAG_FILE`（默认与 `MAPPING_FILE` 同目录的 `diagnostics.json`，tmp+rename 原子替换）。

重启后：

1. **环原样恢复**（记录、`seq` 序号连续）。
2. 若进程退出时某设备处于 `running`/`stopping`，一律按 **crash = stop** 处理：恢复为 `idle`，**不自动重启采样循环**——采样是对设备的主动探测，必须由 engineer 显式再次 `start`。
3. 状态文件缺失或损坏不会拖垮网关：从空状态启动，并在 `GET /api/diagnostics` 的 `loadError` 字段暴露问题。

设备列表支持按诊断指标排序：`GET /api/devices?sort=lastFailure`（最近失败者优先）或 `?sort=avgDuration`（平均耗时高者优先）；无样本/无失败的设备排最后。

## YAML DSL

支持 `float32_abcd` / `float32_cdab` / `int16` / `uint16` / `bool_bit`；`scale`/`offset`；写回逆运算并校验 `min`/`max`。

默认设备 `plc-line-a`，点位含 `motor_rpm`、`temperature`、`pressure`、`status_word`、`run_flag`、`setpoint`。

## Verification

```bash
# 1) 登录
TOKEN=$(curl -s -X POST http://localhost:8175/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"engineer","password":"mod123456"}' | jq -r .token)

# 2) snapshot
curl -s http://localhost:8175/api/devices/plc-line-a/snapshot \
  -H "Authorization: Bearer $TOKEN" | jq .

# 3) 写 motor_rpm
curl -s -X PUT http://localhost:8175/api/devices/plc-line-a/points/motor_rpm \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":1800}' | jq .

# 4) 再次 snapshot 确认写回
curl -s http://localhost:8175/api/devices/plc-line-a/snapshot \
  -H "Authorization: Bearer $TOKEN" | jq '.points[] | select(.name=="motor_rpm")'

# 5) 非法 reload 应失败并保留旧配置
curl -s -X POST http://localhost:8175/api/reload \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"yaml":"devices: []"}' | jq .

# 6) 启动采样（engineer）；重复 start 应返回 409
curl -s -X POST http://localhost:8175/api/diagnostics/plc-line-a/start \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"intervalMs":1000}' | jq .
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8175/api/diagnostics/plc-line-a/start \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"intervalMs":1000}'   # => 409

# 7) 查看诊断矩阵与采样环（手动 snapshot 与采样器记录都在）
curl -s http://localhost:8175/api/diagnostics -H "Authorization: Bearer $TOKEN" | jq .
curl -s 'http://localhost:8175/api/diagnostics/samples?limit=5' -H "Authorization: Bearer $TOKEN" | jq .

# 8) 设备列表按最近失败 / 平均耗时排序
curl -s 'http://localhost:8175/api/devices?sort=lastFailure' -H "Authorization: Bearer $TOKEN" | jq '.devices[].id'

# 9) 停止采样；重启 backend 容器后环仍在、采样状态恢复为 idle 且不自动重启
curl -s -X POST http://localhost:8175/api/diagnostics/plc-line-a/stop \
  -H "Authorization: Bearer $TOKEN" | jq .
docker compose restart backend
curl -s http://localhost:8175/api/diagnostics -H "Authorization: Bearer $TOKEN" | jq .
```

observer 登录后只能读 `/api/diagnostics*` 与 `/api/devices`，`start`/`stop` 返回 403。

浏览器路径：登录 → 设备列表 → 点位监控（看 snapshot）→ 写 `motor_rpm` → 映射配置页提交非法 YAML 应提示保留旧配置。

## 本地开发（可选）

```bash
# mock-plc
python mock-plc/server.py

# backend
cd backend && go run ./cmd/server

# frontend
cd frontend && npm install && npm run dev
```

## 单测

```bash
cd backend && go test ./...
```
