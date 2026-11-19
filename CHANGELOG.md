# Changelog

## Changed

- A device ticket cannot occupy the host socket. Each binding has its own
  device socket. Redeem checks the host socket before it consumes a code, and
  it does not consume the code when the host is offline, the 32-binding cap is
  full, or the device key is the host key. Re-pairing the same host and device
  retires the old binding and drops that socket. Revoke does the same.
- One host public key belongs to one token. A second token cannot register it.
  `BindingByPeers` returns the newest live row. `LookupPairing` does not
  consume a code, and `RestorePairing` undoes a consume after a failed redeem.
- `GET /trace/{id}` with a host token only returns that host's own records.
  Admin trace stays unscoped. WebSocket control pings do not extend the idle
  deadline. Forwarded TypeData is not retained unless a test calls `EnableSniff`.
- The client accepts a handshake only on the relay, and ignores a repeat once
  the link exists. A punch packet switches to direct only when its UDP source
  is in the sealed disco set. `TypeObserved` is accepted only from the hub on
  the relay. Session nonces start at 1 and reject replay or an older nonce.
  Redeem compares the response host key with the offer. The phone page pins
  the QR host key and sends its version.
- Admin snapshot `hosts[].online` is the host websocket. It stays true with
  zero bindings and with every phone offline. `bindings[].online` is that
  binding's device socket. `Hub.PeerOnline(pub)` is true when this process
  has a host socket or a device socket for that public key.
- `Hub.PeerOnline` reports whether this process still has that public key's
  websocket. `LinkPath` stays empty without a fresh TypePath; callers that
  mean "the phone is connected" must read `PeerOnline` and not treat an empty
  path as offline.
- Embedders implement `store.Store` on their own database and read
  `Hub.LinkPath`. They do not take pairlink's SQLite file or `/pairlink/admin`.

## Added

- `examples/direct` starts a loopback hub and two UDP endpoints in one
  process, then holds the admin page on `direct` (直连) until interrupted.
- Register, redeem, and `TypeLabel` carry a caller-supplied `version`. A host
  socket stores the PC build string. A device socket stores the phone software
  version on that device's bindings. An empty field does not clear a stored
  version. The hub does not invent one. Admin snapshot and `GET /bindings`
  show both ends.
- `TypeLabel` (`0x06`) announces a host name or a device name and model on the
  relay socket after it connects, and again when the labels change. The hub
  stores the frame and does not forward it. An empty sanitized field does not
  clear a stored label. A device must send `name` and `model` as separate
  fields; a glued name leaves `model` empty. `store.SetDeviceLabels` updates
  every binding for that device public key.
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

- The hub clock is safe to swap while a socket goroutine is reading it.
- Handover and process exit can close every relay WebSocket via `ClosePeers`.
  A device that upgrades while its host is not in this process is closed at
  once, so the client redials instead of sending into an empty peer table.

