# Quota Schedule Refresh

CLIProxyAPI plugin that sends one Codex quota-window refresh request per credential at a configured daily time.

Plugin ID / binary / config key: `quota-schedule-refresh`.

Author: ssgs. Repository: https://github.com/glj2369/quota-schedule-refresh

## Config

| Field | Meaning |
| --- | --- |
| `schedule_enabled` | Daily timer switch |
| `daily_at` | `HH:MM`, default `08:00` |
| `timezone` | Default `Asia/Shanghai` |
| `model` | Enum from CPA `GET /v1/models` (OpenAI/Codex, non-image). Empty uses the first listed model |
| `request_method` | `direct` = `host.http.do` to Codex; `cpa` = `host.model.execute` |
| `timeout_seconds` | Per-request timeout |
| `enable_disabled` | Re-enable disabled credentials before refresh |
| `max_concurrency` | Worker pool size |
| `prompt` | Refresh prompt, default `hello` |

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
      request_method: direct
      timeout_seconds: "60"
      enable_disabled: true
      max_concurrency: 2
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
quota-schedule-refresh_0.2.0_linux_amd64.zip
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
- Run now: `POST /v0/management/quota-schedule-refresh/run`
