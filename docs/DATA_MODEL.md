# 数据模型

`store.Store` 是唯一的持久化边界。内存和 SQLite 实现同一组方法。业务包、明文、私钥、口令原文都不在这里。

## hosts

| 字段 | 含义 |
|---|---|
| `pub` | 主机公钥，32 字节。登记前为空 |
| `token_hash` | Host Token 的 SHA-256 |
| `name` | 显示名，通常是主机名。可空 |
| `created` | 创建时间 |

空公钥不得把两条未登记主机并成一条。匹配时先看 token 哈希，再看 32 字节公钥。

## pairings

| 字段 | 含义 |
|---|---|
| `id` | 配对 id，可拿去 trace |
| `host_pub` | 签发这张码的主机 |
| `code_hash` | 配对码的 SHA-256 |
| `expires` | 过期时间 |
| `consumed` | 兑换后为真 |
| `session_id` | 16 字节会话 id，不是密钥 |

## bindings

| 字段 | 含义 |
|---|---|
| `id` | 绑定 id |
| `host_pub` / `device_pub` | 两端公钥 |
| `ticket_hash` | Device Ticket 的 SHA-256 |
| `device_name` | 兑换时的 `name` |
| `device_model` | 兑换时的 `model` |
| `revoked` | 吊销 |
| `created` | 创建时间 |
| `session_id` | 与配对相同的会话 id |

每个主机最多 32 条未吊销绑定。吊销的仍留在 `ListAllBindings` 里，主机自己的 `ListBindings` 不返回它们。

## traces

| 字段 | 含义 |
|---|---|
| `ref` | 配对 id、会话 id 或指纹 |
| `at` | 时间 |
| `kind` | `register` `pair` `bind` `connect` `forward` `disconnect` |
| `peer_fp` | 对端指纹 |
| `bytes` | payload 长度，不是内容 |
| `path` | 转发那一跳写 `relay`。实时数据面不在这列 |
| `note` | 短备注，不得含秘密 |

全局大约保留最近 4000 条。

## 不入库

- 在线与否：进程里的 WebSocket 表
- `relay` / `direct`：`Hub.LinkPath`，45 秒过期

重启后名字还在，路径要等端点再宣告。
