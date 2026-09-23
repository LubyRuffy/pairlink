# 命令

地址都是 flag。把示例里的 host 换成你这次要听的地址。

## pairlinkd

```bash
go run ./cmd/pairlinkd -listen 0.0.0.0:7780 -udp 0.0.0.0:7781 -database pairlink.db
```

stderr 会打出：

- 管理页和手机页的 URL
- 库是空的时候，一把只显示一次的主机口令
- 没配管理口令时，一把只显示一次的管理口令

再次启动且库里已有主机时，不再打印主机口令。丢了就用管理页「签发主机口令」，或 `POST /pairlink/v1/admin/hosts`。

```bash
go run ./cmd/pairlinkd -config configs/pairlink.example.yaml -database pairlink.db
```

手机实时摄像头：

```bash
go run ./cmd/pairlinkd -listen 0.0.0.0:7780 -udp 0.0.0.0:7781 -database pairlink.db -tls
```

## PC

```bash
go run ./examples/pc -hub http://127.0.0.1:7780 -token '<主机口令>'
```

`-name` 省略时用操作系统主机名。页面默认开在 `127.0.0.1:7790`，给手机看二维码。hub 如果是 `-tls` 起的：

```bash
go run ./examples/pc -hub https://<本机IP>:7780 -token '<主机口令>' -ca pairlink-cert.pem
```

`-insecure` 跳过证书校验。不要和 `-ca` 一起用。

终端会打印配对 URI，方便摄像头失败时粘贴。URI 里有配对码，别贴到日志系统里。

## 手机页

跟 hub 一起走就行：`http://<hub>/demo/mobile`。

单独进程：

```bash
go run ./examples/mobile -listen 127.0.0.1:7791
```

页面打开 `/demo/mobile`。二维码里的 hub 决定兑换打到哪。

## echo

两端都是 Go 客户端，用来看直连：

```bash
go run ./examples/echo -role host -hub http://127.0.0.1:7780 -token '<主机口令>'
go run ./examples/echo -role device -hub http://127.0.0.1:7780 -offer '<uri>'
```

回环上路径会升到 `direct`。UDP 断了会回到 `relay`。
