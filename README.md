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
quota-schedule-refresh_0.7.9_linux_amd64.zip
checksums.txt
```

## 配置

设置在插件管理页的「设置」页签内维护，保存后立即生效，并写入插件自己的配置文件：

```text
<工作目录>/plugins/quota-schedule-refresh/settings.json
```

官方镜像只挂载 `plugins`、`config.yaml`、`auths` 和 `logs`，所以设置写在 `plugins` 目录下，
容器重建后仍然保留。该目录不存在时回落到 `<用户配置目录>/CLIProxyAPI/quota-schedule-refresh/settings.json`。
也可以用环境变量 `QUOTA_SCHEDULE_REFRESH_SETTINGS` 指定完整路径。

插件不再向 CPA 声明 `config_fields`，因此 CPA 插件详情页不会生成配置表单。宿主的
`config_fields` 协议只有 `name`、`type`、`enum_values`、`description` 四项，无处表达默认值，
自动生成的表单会把未填写的项显示成空值、布尔项一律显示成关闭，与插件实际生效的默认值不一致。

`config.yaml` 中的同名字段仍然可用，作为首次启动的基线；页面保存过之后以 `settings.json` 为准。

| 字段 | 说明 |
| --- | --- |
| `schedule_enabled` | 是否启用定时刷新 |
| `daily_at` | 触发时间，`HH:MM`，默认 `08:00` |
| `timezone` | 时区，默认 `Asia/Shanghai` |
| `model` | Codex 模型，空则使用列表第一项 |
| `timeout_seconds` | 单次请求超时（秒），默认 `60` |
| `enable_disabled` | 刷新前启用已禁用的凭证 |
| `skip_gpt_pro` | 定时刷新跳过 GPT Pro 凭证，默认开启；页面手动勾选仍会执行 |
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
      retry_count: 2
      retry_interval_seconds: 2
      prompt: "hello"
```

凭证逐个刷新，没有并发配置项：刷新前要临时提升目标凭证的优先级并把它设为 CPA 的首选账号，
这些都是全局状态，并行执行会互相覆盖。

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
| 读取设置 | `GET /v0/management/quota-schedule-refresh/settings` |
| 保存设置 | `PUT /v0/management/quota-schedule-refresh/settings` |
| 手动刷新 | `POST /v0/management/quota-schedule-refresh/run` |

保存设置请求体：`{ "settings": { "daily_at": "08:00", ... } }`，字段名与上表一致。

手动刷新请求体：`{ "auth_ids": ["..."] }`，空则刷新全部 Codex 凭证。
