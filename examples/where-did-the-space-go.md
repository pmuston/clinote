---
notekit: 1
title: Where did the space go?
notekit-tool: clinote
width: full
---

An investigation you can re-run. Cell 2 is the slow one; everything below it reads the
snapshot it writes, so the reporting can be reworked without paying for the walk again.

Set `ROOT` before starting clinote to look somewhere other than your home directory.

## 1. Pick a target

```sh
ROOT="${ROOT:-$HOME}"
echo "looking at $ROOT"
```

## 2. Walk it once — the slow cell

```sh
time du -d1 -k "$ROOT" 2>/dev/null | sort -nr > du-snapshot.tsv
printf '%s directories\n' "$(wc -l < du-snapshot.tsv | tr -d ' ')"
```

## 3. The biggest ten

```sh {format=csv}
{ echo "mib,path"
  awk 'NR>1 && NR<=11 { printf "%.0f,%s\n", $1/1024, $2 }' du-snapshot.tsv
}
```

## 4. How much of the total is that?

```sh
awk 'NR==1 { t=$1 } NR>1 && NR<=11 { s+=$1 }
     END { printf "top ten = %.0f%% of %.1f GiB\n", 100*s/t, t/1048576 }' du-snapshot.tsv
```

## 5. Drill into the worst one

```sh
BIG=$(awk 'NR==2 { print $2 }' du-snapshot.tsv)
echo "$BIG"
du -d1 -k "$BIG" 2>/dev/null | sort -nr | sed -n '2,6p'
```

## 6. du already speaks TSV

```sh {format=tsv}
{ printf 'kib\tpath\n'; sed -n '2,6p' du-snapshot.tsv; }
```

## 7. Anything obviously reclaimable?

```sh
awk '{ print $2 }' du-snapshot.tsv | grep -ci cache
```
