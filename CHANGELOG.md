# Changelog

## Changed

- `Hub.PeerOnline` reports whether this process still has that public key's
  websocket. `LinkPath` stays empty without a fresh TypePath; callers that
  mean "the phone is connected" must read `PeerOnline` and not treat an empty
  path as offline.
- Embedders implement `store.Store` on their own database and read
  `Hub.LinkPath`. They do not take pairlink's SQLite file or `/pairlink/admin`.

## Added

- `store.NoteDeviceSeen` records the latest device-websocket attach or drop on
  every binding for that device public key, including revoked rows. A host
  key, an unknown key, a zero time, or an older time changes nothing. The hub
  calls it when a device socket is accepted and again when that socket drops.
  Keepalives do not. `last_connected_at` is on the admin snapshot and on
  `GET /bindings` when set; empty means never observed and is not the
  binding's created time.
- SQLite `store.Store`, admin JSON (`/pairlink/v1/admin/snapshot` and friends),
  and `/pairlink/admin`. Snapshot shows host name, device name, device model,
  and the live `relay` or `direct` path. Raw tokens are not in that JSON.
- `pairlinkd -database`, `-admin-token`, `-config`, `-tls`. PC demo
  `examples/pc` registers the hostname and shows a QR. Phone page
  `/demo/mobile` scans and redeems with a name and model.
- Redeem accepts optional `model`. `GET /bindings` returns `device_model`,
  `online`, and `path`.
- `POST /pairlink/v1/hosts` and `POST /pairlink/v1/pairings/redeem` accept an
  optional `name`. The hub stores a sanitized 80-rune host label and device
  label. An empty name does not clear a label already stored. `GET /bindings`
  returns `device_name`. These strings are not read from TypeData.
- Clients announce `relay` or `direct` on the relay socket when the data plane
  changes and on every keepalive. `Hub.LinkPath` keeps that observation for
  45s. A TypeData payload is never a path, and an unbound or stale pair stays
  empty so a caller can show offline.
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

