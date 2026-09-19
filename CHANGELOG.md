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
