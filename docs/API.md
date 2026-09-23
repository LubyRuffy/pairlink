# HTTP API

Base path `/pairlink/v1`. JSON bodies. Capacitor / browser `fetch` is allowed:
`OPTIONS` returns 204 and every response includes `Access-Control-Allow-Origin: *`.
No cookies, no Gateway Key.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/hosts` | body `token` | bind host public key to a Host Token. Optional `name` is the host's own label (hostname or display name), sanitized to 80 runes. Empty `name` does not clear a stored label |
| POST | `/pairings` | Bearer host token | mint a one-time scan code |
| POST | `/pairings/redeem` | none | spend code, returns device ticket. Optional `name` and `model` are the device's own labels |
| GET | `/ws` | Bearer host token or device ticket (or `Sec-WebSocket-Protocol: pairlink.ticket.<ticket>` for browser / Capacitor clients that cannot set Authorization) | ciphertext relay |
| GET | `/info` | none | advertised STUN address from this request's Host + UDP port |
| GET | `/bindings` | Bearer host token | fingerprints, `device_name`, `device_model`, `online`, `path` (`relay`, `direct`, or empty) |
| POST | `/bindings/{id}/revoke` | Bearer host token | revoke |
| GET | `/trace/{id}` | Bearer host token | metadata timeline |
| GET | `/pairlink/admin` | none (API calls need the admin token) | HTML management page. 404 when admin is disabled |
| GET | `/admin/snapshot` | Bearer admin token | hosts (name, online) and bindings (name, model, path). No tokens, codes, or tickets |
| POST | `/admin/hosts` | Bearer admin token | issue a host token; raw token is only in this response |
| POST | `/admin/bindings/{id}/revoke` | Bearer admin token | revoke any binding |
| GET | `/admin/trace/{id}` | Bearer admin token | same metadata timeline as `/trace/{id}` |

WebSocket and UDP frames: see `protocol.Frame`. TypeData payloads are opaque.
`name` on register and redeem, and `model` on redeem, are operator-visible
coordination metadata, not session plaintext. The hub does not read a name
out of TypeData. `path` on a binding is the latest `TypePath` announcement
(`relay` or `direct`) for 45s. A forward trace event with `path=relay` only
means that hop crossed the hub.

Admin routes 404 until `Hub.SetAdminToken` (or `pairlinkd -admin-token` /
`PAIRLINK_ADMIN_TOKEN`). The HTML page is `GET /pairlink/admin`; the JSON
routes above are under `/pairlink/v1`.
