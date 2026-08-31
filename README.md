# Quota Schedule Refresh

CLIProxyAPI 插件：每天按设定时刻，对每个 Codex 凭证发送一次额度窗口刷新请求。

插件 ID / 二进制 / 配置键：`quota-schedule-refresh`。

作者：ssgs。仓库：https://github.com/glj2369/quota-schedule-refresh

刷新只走 CPA 的 `host.model.execute`，不直连上游。

## 配置

| 字段 | 含义 |
| --- | --- |
| `schedule_enabled` | 启用定时刷新 |
| `daily_at` | 每天触发时刻 `HH:MM`，默认 `08:00` |
| `timezone` | 时区，默认 `Asia/Shanghai` |
| `model` | 从 CPA `GET /v1/models` 读取的 Codex 模型。空则用第一项 |
| `timeout_seconds` | 单次请求超时（秒）；每次重试单独计时 |
| `enable_disabled` | 刷新前自动启用已禁用凭证 |
| `max_concurrency` | 同时刷新账号数上限 |
| `retry_count` | 失败后额外重试次数，默认 2（最多共 3 次）。0 表示不重试 |
| `retry_interval_seconds` | 重试间隔秒数，默认 2 |
| `prompt` | 刷新提示词，默认 `hello` |

Manager Plus 里的 **priority** 由宿主管理。本插件不是模型路由或 Provider，改 priority 不会改变刷新行为。

## 示例

```yaml
plugins:
  configs:
    quota-schedule-refresh:
      enabled: true
      schedule_enabled: true
      daily_at: "08:00"
      timezone: "Asia/Shanghai"
      model: "gpt-5.6-sol"
      timeout_seconds: "60"
      enable_disabled: true
      max_concurrency: 2
      retry_count: 2
      retry_interval_seconds: 2
      prompt: "hello"
```

## 从插件商店安装

1. 把本仓库的 registry 加到 `plugins.store-sources`：

```yaml
plugins:
  store-sources:
    - https://raw.githubusercontent.com/glj2369/quota-schedule-refresh/main/registry.json
```

2. 在 CPA Manager Plus 中安装 `quota-schedule-refresh`，或：

```text
POST /v0/management/plugin-store/quota-schedule-refresh/install
```

已安装时请点「更新」，不要点「重新安装」（重新安装会清空自定义配置）。

Release 资源按官方商店布局：

```text
quota-schedule-refresh_0.6.4_linux_amd64.zip
checksums.txt
```

## 编译

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -o quota-schedule-refresh.so .
rm -f quota-schedule-refresh.h
```

把二进制放到 `plugins/linux/amd64/quota-schedule-refresh.so`。

## 管理接口

- 页面：`/v0/resource/plugins/quota-schedule-refresh/status`
- 状态：`GET /v0/management/quota-schedule-refresh/status`
- 凭证：`GET /v0/management/quota-schedule-refresh/auth-files`
- 刷新选中凭证：`POST /v0/management/quota-schedule-refresh/run`，body 为 `{ "auth_ids": ["..."] }`
