# Working on pairlink

```bash
make test-race
make check
```

- No business types (no Project, Thread, Turn).
- Hub forwards TypeData without parsing the payload. Host and device labels,
 and the caller-supplied PC and phone versions, are control-plane fields on
 register, redeem, and TypeLabel. They are not a TypeData, TypeHandshake, or
 TypeDisco body. The hub does not invent a version.
- Never log host tokens, pairing codes, tickets, or session keys.
- Listen addresses and hub URLs are flags/config, never compiled-in hostnames.
- A path implementation that cannot fall back to relay is unfinished.
- Files stay under 1000 lines.
