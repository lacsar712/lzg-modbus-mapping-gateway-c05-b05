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
- `GET  /api/devices`
- `GET  /api/devices/{id}/points`
- `GET  /api/devices/{id}/points/{name}`
- `PUT  /api/devices/{id}/points/{name}` body `{ "value": <number> }`
- `GET  /api/devices/{id}/snapshot`

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
```

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
