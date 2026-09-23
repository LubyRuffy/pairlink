# pairlink

Scan-to-bind, then talk. Traffic starts on a DERP-style hub and upgrades to a
direct UDP path when hole punching works — the Tailscale path model, not a VPN.

The hub never sees application plaintext. Peers are long-term X25519 public
keys. A host sends `name` (usually the hostname) on register. A device sends
`name` and `model` on redeem. Those labels are coordination metadata, not a
decrypted frame. Binding is a QR URI:

```
pairlink:v1:<hub_url>:<pairing_code>:<host_spk>
```

`hub_url` comes from configuration. Nothing in this module hardcodes a
deployment hostname.

## Quick start

```bash
go test -race ./...
go run ./cmd/pairlinkd -listen 0.0.0.0:7780 -udp 0.0.0.0:7781 -database pairlink.db
# stderr prints a one-time host token and a one-time admin token
go run ./examples/pc -hub http://127.0.0.1:7780 -token '<host token>'
# phone opens the mobile URL printed by pairlinkd and scans the QR
```

Admin UI: the `admin` URL from stderr. It lists the PC hostname, the device
name and model, and whether the live path is `relay` or `direct`.

`examples/echo` is the two-Go-client path. On loopback it upgrades to
`direct` and falls back to `relay` if UDP dies. The phone page has no UDP, so
it stays on `relay`.

Protocol: [docs/PROTOCOL.md](docs/PROTOCOL.md). Config: [docs/CONFIG.md](docs/CONFIG.md).
Commands: [docs/CLI.md](docs/CLI.md).

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
