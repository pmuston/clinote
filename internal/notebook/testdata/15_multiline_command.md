```sh
cat <<EOF |
first line
second line
EOF
  tr '[:lower:]' '[:upper:]' |
  sort -r
```
