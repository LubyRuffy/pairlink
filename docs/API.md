# HTTP API

Base path `/pairlink/v1`. JSON bodies. Capacitor / browser `fetch` is allowed:
`OPTIONS` returns 204 and every response includes `Access-Control-Allow-Origin: *`.
No cookies, no Gateway Key.

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/hosts` | body `token` | bind host public key to a Host Token |
| POST | `/pairings` | Bearer host token | mint a one-time scan code |
| POST | `/pairings/redeem` | none | spend code, returns device ticket |
| GET | `/ws` | Bearer host token or device ticket (or `Sec-WebSocket-Protocol: pairlink.ticket.<ticket>` for browser / Capacitor clients that cannot set Authorization) | ciphertext relay |
| GET | `/info` | none | advertised STUN address from this request's Host + UDP port |
| GET | `/bindings` | Bearer host token | list bound device fingerprints |
| POST | `/bindings/{id}/revoke` | Bearer host token | revoke |
| GET | `/trace/{id}` | Bearer host token | metadata timeline |

WebSocket and UDP frames: see `protocol.Frame`. TypeData payloads are opaque.
