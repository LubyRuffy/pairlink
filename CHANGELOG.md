# Changelog

## Added

- Scan-to-bind offer URI and QR PNG helper.
- Embeddable DERP-style hub: register, pairing, redeem, WebSocket forward, STUN-lite.
- Client path selection: relay first, UDP upgrade, fallback when UDP dies.
- End-to-end ChaCha20-Poly1305 sessions; hub cannot open application payloads.
- `pairlinkd` and `examples/echo`.
- Browser / Capacitor WebSocket auth via `pairlink.ticket.<ticket>` subprotocol
  (no ticket in the URL, no Authorization header required).
- CORS on `/pairlink/v1` HTTP so Capacitor WebView `fetch` redeem is not
  `Failed to fetch` after a valid QR paste.
- Host WebSocket keepalive data frames (hub quiet-WS is 60s) and reconnect
  after a dropped socket, so redeem is not `host offline` while the PC still
  mints QR codes over HTTP.

## Fixed

- Handover and process exit can close every relay WebSocket via `ClosePeers`.
  A device that upgrades while its host is not in this process is closed at
  once, so the client redials instead of sending into an empty peer table.

