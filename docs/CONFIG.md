# 配置

`pairlinkd` 的监听地址、库文件和 TLS 来自 flag 或 YAML。没有写进二进制的主机名。

## 优先级

1. 内置默认值
2. `-config` 指向的 YAML
3. 命令行显式传入的 flag
4. 管理口令：显式 `-admin-token` 优先，否则读环境变量 `PAIRLINK_ADMIN_TOKEN`，都没有就生成一把并在 stderr 打一次

## YAML

```yaml
listen: 127.0.0.1:7780
udp: 127.0.0.1:7781
database: pairlink.db
tls: false
tls_cert: pairlink-cert.pem
```

不要把主机口令或管理口令写进这个文件。仓库里的 `configs/pairlink.example.yaml` 也没有口令。

## 字段

| 键 | flag | 默认 | 含义 |
|---|---|---|---|
| `listen` | `-listen` | `127.0.0.1:7780` | HTTP 或 HTTPS 监听 |
| `udp` | `-udp` | `127.0.0.1:7781` | STUN-lite UDP |
| `database` | `-database` | 空 | 空则内存；非空则 SQLite 文件 |
| `tls` | `-tls` | false | 为真时生成一张只含 IP SAN 的临时证书 |
| `tls_cert` | `-tls-cert` | `pairlink-cert.pem` | 把证书（不含私钥）写到这里，给 PC 的 `-ca` |

手机要开摄像头实时扫，页面必须是安全上下文。`-tls` 加上 `-listen 0.0.0.0:7780`，用 stderr 打出来的 `https://<本机IP>:7780/demo/mobile`。拍照选图不依赖 TLS。

`-listen 0.0.0.0` 时，启动日志会列出本机各 IPv4，方便把 hub URL 配进 PC。这些地址是运行时网卡，不是编译进去的。
