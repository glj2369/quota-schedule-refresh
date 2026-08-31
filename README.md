# Quota Schedule Refresh

CLIProxyAPI plugin that sends one Codex quota-window refresh request per credential at a configured daily time.

Plugin ID / binary / config key: `quota-schedule-refresh`.

Author: ssgs. Repository: https://github.com/glj2369/quota-schedule-refresh

Refresh always uses CPA `host.model.execute`. There is no direct upstream path.

## Config

| Field | Meaning |
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

The Manager Plus **priority** field is host-owned. This plugin is not a model router or provider, so changing priority does not change refresh behavior.

## Example

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

## Install from plugin store

1. Add this registry to `plugins.store-sources`:

```yaml
plugins:
  store-sources:
    - https://raw.githubusercontent.com/glj2369/quota-schedule-refresh/main/registry.json
```

2. Install `quota-schedule-refresh` from CPA Manager Plus, or:

```text
POST /v0/management/plugin-store/quota-schedule-refresh/install
```

Release assets follow the official store layout:

```text
quota-schedule-refresh_0.6.4_linux_amd64.zip
checksums.txt
```

## Build

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -o quota-schedule-refresh.so .
rm -f quota-schedule-refresh.h
```

Place the binary at `plugins/linux/amd64/quota-schedule-refresh.so`.

## Management

- Page: `/v0/resource/plugins/quota-schedule-refresh/status`
- Status: `GET /v0/management/quota-schedule-refresh/status`
- Credentials: `GET /v0/management/quota-schedule-refresh/auth-files`
- Run selected: `POST /v0/management/quota-schedule-refresh/run` with `{ "auth_ids": ["..."] }`
