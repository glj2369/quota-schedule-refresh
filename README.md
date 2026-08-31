# Quota Schedule Refresh

[CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 插件。每天在指定时间刷新 Codex 额度窗口。

ID：`quota-schedule-refresh`

## 安装

```yaml
plugins:
  store-sources:
    - https://raw.githubusercontent.com/glj2369/quota-schedule-refresh/main/registry.json
```

在 CPA Manager Plus 插件商店中安装。升级请用「更新」，不要用「重新安装」。

```text
POST /v0/management/plugin-store/quota-schedule-refresh/install
```

发布包：

```text
quota-schedule-refresh_0.6.6_linux_amd64.zip
checksums.txt
```

## 配置

| 字段 | 说明 |
| --- | --- |
| `schedule_enabled` | 是否启用定时刷新 |
| `daily_at` | 触发时间，`HH:MM`，默认 `08:00` |
| `timezone` | 时区，默认 `Asia/Shanghai` |
| `model` | Codex 模型，空则使用列表第一项 |
| `timeout_seconds` | 单次请求超时（秒），默认 `60` |
| `enable_disabled` | 刷新前启用已禁用的凭证 |
| `skip_gpt_pro` | 定时刷新跳过 GPT Pro 凭证，默认开启；页面手动勾选仍会执行 |
| `max_concurrency` | 并发上限 |
| `retry_count` | 失败重试次数，默认 `2` |
| `retry_interval_seconds` | 重试间隔（秒），默认 `2` |
| `prompt` | 刷新用的提示词，默认 `hello` |

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
      skip_gpt_pro: true
      max_concurrency: 2
      retry_count: 2
      retry_interval_seconds: 2
      prompt: "hello"
```

## 编译

```bash
CGO_ENABLED=1 go build -buildmode=c-shared -o quota-schedule-refresh.so .
rm -f quota-schedule-refresh.h
```

输出放到 `plugins/linux/amd64/quota-schedule-refresh.so`。

## 接口

| 说明 | 路径 |
| --- | --- |
| 管理页 | `/v0/resource/plugins/quota-schedule-refresh/status` |
| 状态 | `GET /v0/management/quota-schedule-refresh/status` |
| 凭证列表 | `GET /v0/management/quota-schedule-refresh/auth-files` |
| 手动刷新 | `POST /v0/management/quota-schedule-refresh/run` |

手动刷新请求体：`{ "auth_ids": ["..."] }`，空则刷新全部 Codex 凭证。
