---
title: S88
created: 2026-07-22T03:37:10Z
shell: bash
editable: true
width: full
requires: 
 - NEO4J_PW
---

# S88

```sh out=jsonl
cyq --password "$NEO4J_PW" --query "MATCH (n:Node) RETURN n" --format jsonl | gq .properties
```

```output type=jsonl exit=3 ran=2026-07-22T07:40:17Z dur=36ms
rows=15 elapsed=14ms
gq: usage: unexpected argument ".properties"
```

