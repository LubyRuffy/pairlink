# Working on pairlink

```bash
make test-race
make check
```

- No business types (no Project, Thread, Turn).
- Hub forwards TypeData without parsing the payload.
- Never log host tokens, pairing codes, tickets, or session keys.
- Listen addresses and hub URLs are flags/config, never compiled-in hostnames.
- A path implementation that cannot fall back to relay is unfinished.
- Files stay under 1000 lines.
