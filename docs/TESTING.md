# Testing

```bash
go test -race ./...
```

Required cases:

- Offer URI round-trip with a colon in the hub URL.
- Session seal/open; a third identity cannot open the ciphertext; plaintext is
  absent from the wire bytes the hub forwards.
- QR PNG is a real PNG produced from a parseable URI.
- Two clients, UDP disabled: echo works, `path=relay`.
- Two clients on loopback: path upgrades to `direct`, then `KillUDP` falls back
  and echo still works. The hub's `LinkPath` follows those announcements.
  A TypeData byte that matches the direct code does not create a path;
  an overlong or unbound announcement is ignored; a 45s-old observation expires.
- Trace for a session id contains bind/forward and not the payload.
- Expired pairing redeem fails.
- Device ticket as WebSocket subprotocol opens the relay (browser clients).
- Capacitor WebView OPTIONS preflight on redeem returns 204 with CORS.
- Host keepalive frames hold a quiet WebSocket past the hub idle deadline;
  a dropped socket reconnects and redeem still works.
- `ClosePeers` closes every upgraded relay socket without waiting for the idle
  deadline. A device upgrade while its host is absent is closed immediately
  instead of staying open and dropping frames.
- Register stores a host label; an empty label does not wipe it; overlong
  labels clip at 80 runes.
- Redeem stores device `name` and `model`. A TypeData payload does not change
  the host label.
- Admin snapshot requires the admin token, returns hostname / device name /
  model / `direct` then `relay`, and does not contain the host token, pairing
  code, or device ticket. Admin disabled is 404.
- SQLite reopens with the same host name and device model. Two hosts with
  empty public keys stay two rows.
- Phone QR upload of a generated offer PNG round-trips the URI. A non-QR image
  is rejected without echoing bytes.
