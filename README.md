# htokenmeter

自托管 AI 网关，为 OpenWrt 路由器/软路由而设计：把 OpenAI、Anthropic、Gemini
及一切 OpenAI 兼容服务（DeepSeek、Moonshot、Ollama、vLLM、OpenRouter…）
归集为一个内网统一入口，在同一个控制台管理渠道、访问令牌、用量与成本。

面向家庭与个人：单二进制、仅 SQLite、无 Redis/MySQL/多节点依赖，开箱即用。

## 功能

| 能力 | 说明 |
|---|---|
| 多协议入口 | `/v1/chat/completions`、`/v1/completions`、`/v1/embeddings`、`/v1/images/generations`、`/v1/moderations`、`/v1/responses`（含 GET）、**Claude 原生 `/v1/messages`**、**Gemini 原生 `/v1beta/models/{model}:{action}**` |
| 一把钥匙三种 SDK | 鉴权头归一化：OpenAI `Authorization`、Claude `x-api-key`、Gemini `x-goog-api-key`/`?key=` 均可 |
| 跨格式转换 | 任一入口 → 任一渠道：OpenAI⇄Claude⇄Gemini 互转（非流式全支持；流式支持 O⇄C、O⇄G；同格式走零拷贝直通） |
| 渠道管理 | 4 种类型（OpenAI 兼容/Anthropic/Gemini/Azure）、优先级+权重、模型映射、多 key 轮询与单 key 熔断、渠道级代理、定时测试、自动禁用/恢复 |
| 访问控制 | `sk-` 令牌：过期、模型白名单、IP 网段、美元预算上限 |
| 成本计量 | 内置+可编辑模型单价表（USD/1M tokens），按真实 usage 记账（缓存读折价），看板按日/模型/渠道/令牌聚合 |
| 控制台 | 内嵌 React+AntD 控制台（登录/看板/渠道/令牌/日志/价格/用户）+ LuCI 集成页 |
| 数据保护 | 日志保留期自动清理（防闪存写穿）、SQLite WAL、ability 表启动自修复 |

## 快速开始（本机）

```bash
make build
./htokenmeter --port 8317 --config-dir ./data
```

首次启动会在日志打印管理员随机密码（`first boot` 行）。浏览器打开
`http://localhost:8317` 登录，然后：

1. **渠道**页添加上游（如 Ollama：类型选 OpenAI 兼容，Base URL 填 `http://NAS_IP:11434`）
2. **令牌**页创建 `sk-` key（可限模型/预算/IP）
3. 客户端把 Base URL 指向 `http://路由器IP:8317/v1`，用该 key 调用

## OpenWrt 安装

### 方式 A：Release ipk

```bash
opkg install htokenmeter_*.ipk
opkg install luci-app-htokenmeter_*.ipk   # 可选：LuCI 集成
uci set htokenmeter.config.enabled=1 && uci commit htokenmeter
/etc/init.d/htokenmeter start
```

LuCI → 服务 → htokenmeter：Console 页打开控制台（HTTPS LuCI 下用"新标签页打开"），Base Setting 页改端口/日志/保留期。

### 方式 B：源码集成进固件

仓库即 feed（OpenAppFilter 约定）：直接 clone 进 `package/`，无需拷贝。

```bash
git clone https://github.com/lxp/htokenmeter package/htokenmeter
make menuconfig   # Network -> AI Gateway -> htokenmeter / luci-app-htokenmeter
# 或：echo "CONFIG_PACKAGE_luci-app-htokenmeter=y" >> .config && make defconfig
make package/luci-app-htokenmeter/compile V=s   # 单独编译两个包
# 或整固件集成：make -j$(nproc)
```

`init` 脚本会在 `/etc/config/htokenmeter` 缺失时自动生成默认配置（enabled=0）。

### 设备要求

- **内存 ≥ 256MB**（aarch64：R2S/R4S/R5S/N1 等；x86_64 软路由）
- **闪存 ≥ 128MB**（二进制约 27MB；数据库建议放 extroot：UCI `db_path` 指向外置存储）

## 环境变量 / 参数

| 参数 | 环境变量 | 默认 | 说明 |
|---|---|---|---|
| `--port` | `PORT` | 8317 | 监听端口 |
| `--config-dir` | `CONFIG_DIR` | /etc/htokenmeter | 数据目录 |
| `--db` | `DB_PATH` | `<config-dir>/htokenmeter.db` | SQLite 路径 |
| `--log-dir` | `LOG_DIR` | 空（stderr） | 日志目录 |
| - | `RETRY_TIMES` | 3 | 429/5xx 重试次数 |
| - | `LOG_RETENTION_DAYS` | 30 | 日志保留天数 |
| - | `TEST_FREQUENCY_MINUTES` | 0（关） | 定时渠道测试 |

## 已知边界

- Responses API（`/v1/responses`）仅支持 OpenAI 兼容渠道直通；跨格式走 chat 语义转换
- Claude⇄Gemini 跨格式**流式**不支持（非流式可用），会返回明确的 400 提示
- token 估算在无上游 usage 时用字符近似（仅影响预算预检，结算以真实 usage 为准）

## 架构

```
请求 → TokenAuth(头归一化) → Distribute(ability 优先级+加权)
     → Relay: 解析(按协议) → 转换(矩阵) → 渠道(重试环) → 响应(直通/转换+usage提取)
     → 记账: usage × 单价 → logs → 令牌/渠道累计
```

数据表：users / tokens / channels / abilities / options / logs / model_prices。

许可：GPL-3.0-or-later（见 [LICENSE](LICENSE)）。架构参考与许可边界见 [NOTICE](NOTICE)。
