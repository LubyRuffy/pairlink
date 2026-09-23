# pairlink 交互协议 v1

本文只定义两端怎么配对、流量怎么选路、hub 允许看见什么。应用字节是密文，不在本协议里解释。

## 角色

| 角色 | 是谁 | 长期身份 |
|---|---|---|
| Hub | `pairlinkd` 或嵌进别的进程的 `relay.Hub` | 无。它不持有端点私钥 |
| Host | 被扫的那一端，demo 里是 PC | X25519 公钥，32 字节 |
| Device | 扫码的那一端，demo 里是手机 | X25519 公钥，32 字节 |

指纹是公钥 SHA-256 的前 8 字节，十六进制。指纹可以上屏、可以进管理页。下面这些不行：

- Host Token
- 配对码
- Device Ticket
- 会话密钥

hub 只存它们的 SHA-256。日志和 `admin/snapshot` 里不得出现原文。

## 不变量

- Hub 转发 `TypeData` / `TypeHandshake` / `TypeDisco` 时不解析 payload。
- 主机名、设备名、型号走登记、兑换，以及套接字接上后的 `TypeLabel`。塞进 `TypeData`、`TypeHandshake`、`TypeDisco` 的字节不会变成名字。
- 监听地址和 hub URL 来自 flag 或配置。协议里没有写死的部署域名。
- 数据面先走中转。UDP 打洞成功才切直连。直连断了必须回到中转。不能回退的实现是半成品。
- 管理页上的「直连 / 中转」来自端点的 `TypePath` 宣告，不是 hub 对业务包的猜测。

## 标签

`name` 和 `model` 是给人看的字符串。`protocol.SanitizeLabel`：

- 控制字符和换行折成一个空格
- 首尾空白去掉
- 超过 80 个 Unicode 码位就截断
- 结果为空表示「没填」。登记时不得用空串覆盖已经存下的主机名

主机名放在登记的 `name`。设备名称放在兑换的 `name`，型号放在 `model`。两个都可以空，也都可以同时有。

套接字接上之后还可以再宣告一次，不用重新扫码。帧类型是 `TypeLabel`（`0x06`），和 `TypePath` 一样：hub 只落库，不转发给对端，也不把它当成业务包。

payload 是 JSON，名称和型号分开：

```json
{"name":"<label>","model":"<model>"}
```

- 主机套接字只更新自己的主机名。`model` 被忽略。
- 设备套接字按这把设备公钥更新 `device_name` 和 `device_model`。同一台主机上的另一部设备不动。
- 清洗后的空字符串不覆盖已经存过的标签。长度和清洗沿用 `SanitizeLabel`。
- 把系统和型号粘进一个 `name`、`model` 为空时，型号列保持空。hub 不从名字里拆型号。
- 端点在拨号前就知道标签。第一帧跟在套接字接上后面发，标签变了再发一次。不要等加密握手完成再让 hub 猜身份。

不是 JSON 对象的 payload（包括 `TypePath` 那一个字节）直接丢掉。

## 配对 URI

```
pairlink:v1:<hub_url>:<pairing_code>:<host_spk>[?lan=<ip:port>,...]
```

- `hub_url` 可以含冒号。从右边切：最后一段是公钥，倒数第二段是配对码，剩下的是 hub。
- `host_spk` 是主机公钥的 base64url、无填充。
- 配对码不得含 `:` `?` `&`。
- `lan` 是可选的本机 UDP 端点，逗号分隔。
- 配对码默认 10 分钟内可兑换一次。

二维码的像素就是这条 URI。手机解出来之后用 URI 里的 hub，不用页面自己编一个地址。

## 帧

中转 WebSocket 和直连 UDP 用同一种帧。多出来的尾巴是错误，避免一包里夹第二条。

| 偏移 | 长度 | 含义 |
|---|---|---|
| 0 | 4 | ASCII `PLK1` |
| 4 | 1 | 类型 |
| 5 | 32 | 目的公钥 |
| 37 | 32 | 源公钥 |
| 69 | 16 | 会话 id |
| 85 | 4 | payload 长度，大端 |
| 89 | n | payload，最大 64 KiB |

源公钥必须等于这条 WebSocket 认证出来的公钥，否则 hub 丢掉。

| 类型 | 值 | Hub 怎么处理 |
|---|---|---|
| Handshake | `0x01` | 原样转给已绑定的对端。不打开 |
| Data | `0x02` | 原样转发。payload 是密文 |
| Disco | `0x03` | 原样转发。里面是封好的候选地址 |
| Observed | `0x04` | Hub 自己写的，把传输层看到的地址告诉端点 |
| Path | `0x05` | 不转发。payload 必须是 1 字节 |
| Label | `0x06` | 不转发。payload 是 `{"name","model"}`，只落库 |
| PunchPing | `0x10` | 按绑定关系转发，用来保活和打洞 |
| PunchPong | `0x11` | 同上 |

`TypePath` 的唯一合法 payload：

| 字节 | 数据面 |
|---|---|
| `1` | `relay` 中转 |
| `2` | `direct` 直连 |

别的长度、别的值、未绑定的一对公钥，hub 都当没看见。

## 控制面

基路径 `/pairlink/v1`。JSON。浏览器要先发 `OPTIONS`，响应 204，并带 `Access-Control-Allow-Origin: *`。不用 cookie。

### 登记主机

`POST /hosts`

```json
{"pub":"<base64url>","token":"<host token>","name":"<hostname>"}
```

Token 在 body 里，不在 URL 里。成功后这把公钥和这个 token 绑在一起。`name` 可省略。

### 开配对码

`POST /pairings`，`Authorization: Bearer <host token>`。

主机必须已经登记公钥，并且 WebSocket 在线，否则稍后兑换会得到 `host offline`。

响应里的 `code` 只出现这一次。`session_id` 是 16 字节会话 id 的十六进制，后面帧和排障都用它。它不是会话密钥。

### 兑换

`POST /pairings/redeem`，无认证。

```json
{"code":"<pairing code>","device_pub":"<base64url>","name":"<device name>","model":"<model>"}
```

成功返回 `ticket`、`host_pub`、`session_id`、`binding_id`。`ticket` 只出现这一次。码过期、用过、主机不在线、绑定超过 32 个，都要失败，并且失败响应里不要回显码。

### 中转套接字

`GET /ws`

- 主机：`Authorization: Bearer <host token>`
- 设备：同一个头，或者 `Sec-WebSocket-Protocol: pairlink.ticket.<ticket>`

浏览器设不了 WebSocket 的 Authorization，所以走子协议。票不得放进 URL。

设备连上时如果本进程里没有主机的套接字，hub 立刻关掉，让客户端重拨，而不是把帧丢进空表。

端点写出的第一帧是 `TypeLabel`。握手帧里没有可读的主机名。

安静超过 60 秒没读到数据帧，hub 断开。控制帧 ping 不算。端点要周期性写一帧（demo 里大约 15 秒）。

### 绑定和排障

- `GET /bindings`：主机 token。返回指纹、`device_name`、`device_model`、是否在线、当前 `path`。有过设备套接字时还带 `last_connected_at`，没有则省略，不用创建时间填。
- `POST /bindings/{id}/revoke`：主机 token，只能吊销自己的。
- `GET /trace/{id}`：主机 token。`id` 是配对 id、会话 id 或指纹。事件只有元数据：种类、字节数、对端指纹。转发事件的 `path` 固定写 `relay`，因为那一跳确实经过 hub。它不是数据面结论。

`GET /info` 无认证，返回这次请求的 Host 加上 UDP 端口，给 STUN 用。没有写死地址。

## 数据面

1. 两端先在 hub 的 WebSocket 上完成握手。握手字节对 hub 不透明。
2. 应用数据先从中转走。
3. 端点把本机和 STUN 观察到的 UDP 地址封进 `TypeDisco`。hub 另外用 `TypeObserved` 告诉端点「我在传输层看见你从哪来」。hub 不能伪造封好的 disco。
4. 两端互发 `PunchPing` / `PunchPong`。通了就把应用数据改走 UDP，并宣告 `TypePath = direct`。
5. UDP 断了，立刻改回 WebSocket，并宣告 `TypePath = relay`。
6. 路径变化时立刻宣告。没变化也要在保活时再宣告一次。hub 只保留 45 秒。过期后 `LinkPath` 是空串，调用方应显示离线或「无宣告」，不要猜成直连。

浏览器没有原始 UDP。手机网页 demo 只会宣告 `relay`。要看到 `direct`，两端都得是能打洞的客户端（`examples/echo` 或带 UDP 的 Go 客户端），而且打洞得成功。

## 管理面

管理面不是对等数据面。它只读控制面里已经存下的标签，加上进程内存里的在线表和 `TypePath`。

`hosts[].online` 是这台主机的 WebSocket 还在不在本进程。没绑定、手机离线、路径没有新鲜宣告，都不改它。公钥还没登记时它是 false。`bindings[].online` 和 `LinkPath` 只描述手机那一头。嵌入方要看 PC，调用 `Hub.PeerOnline(hostPub)`，不要从绑定列表推断。

持久化接口是 `store.Store`。`Memory` 给测试。`OpenSQLite(path)` 把同一批记录放进 SQLite：主机、配对、绑定、trace。重启后名字还在。路径不入库，进程重启后要等端点再次宣告。`NoteDeviceSeen` 把设备套接字的接上和断开记到绑定的 `last_connected`；主机套接字和保活不写。零值表示没观察过。`SetDeviceLabels` 按设备公钥更新名称和型号，空字段不覆盖。

管理口令是进程配置（`-admin-token` 或 `PAIRLINK_ADMIN_TOKEN`），只存哈希。没配置时下面的路由全部 404。

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/pairlink/admin` | 网页。页面本身不含口令 |
| GET | `/pairlink/v1/admin/snapshot` | 主机名、设备名、型号、在线、`path` |
| POST | `/pairlink/v1/admin/hosts` | 签发一把新的主机口令，响应里只出现一次 |
| POST | `/pairlink/v1/admin/bindings/{id}/revoke` | 吊销任意绑定 |
| GET | `/pairlink/v1/admin/trace/{id}` | 同主机 trace，改用管理口令 |

网页把管理口令放在 `Authorization: Bearer`，不放进 URL。`snapshot` 不得含 token、配对码、ticket。

`path` 取值：

| 值 | 含义 |
|---|---|
| `direct` | 45 秒内有一端宣告直连 |
| `relay` | 45 秒内有一端宣告中转 |
| `""` | 没有新鲜宣告 |

后宣告的覆盖先宣告的。两端应该宣告同一条路。一边直连一边中转会来回跳，那是端点没协商好，不是管理页的第二种状态。

## 一次扫码

1. 操作者向 hub 要一把 Host Token。
2. PC `POST /hosts`，带上公钥和主机名，再 `GET /ws`。
3. PC `POST /pairings`，把 URI 画成二维码。
4. 手机解析 URI，`POST /pairings/redeem`，带上自己的公钥、名称、型号。
5. 手机用 ticket 子协议连上 `/ws`，先发 `TypeLabel`，再周期发送 `TypePath = 1`。
6. 若两端都是 Go 客户端且 UDP 打通，改为宣告 `2`。UDP 失败则继续 `1`。
7. 管理页 `snapshot` 里能看到主机名、设备名、型号，以及 `relay` 或 `direct`。

排障时拿绑定上的 `session_id` 去 `GET /trace/{id}`。事件里没有明文，也没有密钥。
