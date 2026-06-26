#!/usr/bin/env bash
# init.sh — loop de verificación de keeper-sdk-go (harness): build + vet + test.
# El smoke de integración (TestSmoke) hace skip si no hay KEEPER_SMOKE_ENDPOINT.
set -uo pipefail

command -v go >/dev/null || { echo "ERROR: Go no está instalado/PATH."; exit 1; }

fail=0
run() { echo "== $1 =="; shift; "$@" || { echo "   ↑ FALLÓ"; fail=1; }; }

run "go build ./..."  go build ./...
run "go vet ./..."    go vet ./...

# -race exige cgo + compilador C. Si lo hay, se usa; si no (p. ej. Windows sin gcc)
# se omite con aviso. -race es obligatorio en CI/Linux (ADR-0006).
cc="$(go env CC)"
if command -v "$cc" >/dev/null 2>&1; then
  run "go test -race -cover ./..."  env CGO_ENABLED=1 go test -race -cover ./...
else
  echo "== go test -cover ./... =="
  echo "   ⚠ -race OMITIDO: sin compilador C ('$cc') en PATH. Obligatorio en CI/Linux (ADR-0006)."
  CGO_ENABLED=0 go test -cover ./... || { echo "   ↑ FALLÓ"; fail=1; }
fi

if [ "$fail" -ne 0 ]; then
  echo "BASELINE ROJO — corregir antes de apilar trabajo."
  exit 1
fi
echo "BASELINE VERDE ✅"
