# pairlink

Scan-to-bind, then talk. Traffic starts on a DERP-style hub and upgrades to a
direct UDP path when hole punching works — the Tailscale path model, not a VPN.

The hub never sees application plaintext. Peers are long-term X25519 public
keys. A host or device may also send its own display label on register or
redeem (`name`, at most 80 runes). That label is coordination metadata, not
a decrypted frame. Binding is a QR URI:

```
pairlink:v1:<hub_url>:<pairing_code>:<host_spk>
```

`hub_url` comes from configuration. Nothing in this module hardcodes a
deployment hostname.

## Quick start

```bash
go test -race ./...
go run ./cmd/pairlinkd -listen 127.0.0.1:7780 -udp 127.0.0.1:7781
# stderr prints a one-time host token
go run ./examples/echo -role host -hub http://127.0.0.1:7780 -token <token>
go run ./examples/echo -role device -hub http://127.0.0.1:7780 -offer '<uri>'
```

Embed the hub with `relay.New(store).Handler()` under `/pairlink/`.
Go clients send `Authorization: Bearer`. Browser / Capacitor clients send
`Sec-WebSocket-Protocol: pairlink.ticket.<ticket>` instead — never put the
ticket in the URL.

## Troubleshooting

Every pairing and session has an id. `GET /pairlink/v1/trace/<id>` (host token)
returns metadata: connect, pair, bind, forward (byte counts, `path=relay`),
disconnect. Ciphertext and secrets are not stored. Live path is a separate
one-byte `TypePath` announcement from the endpoints. `Hub.LinkPath` reports
`relay` or `direct` for 45s; anything else is absent.
