#!/usr/bin/env bash
set -Eeuo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.1}" GOWORK=off
case "${1:-test}" in
  test)
    go test ./common ./i18n
    go test ./middleware -run 'TestTurnstile' -count=1
    go test ./controller -run '^TestPasswordReset(DestinationAndEscaping|EmailDeliveryAndConfirmation)$' -count=1
    ;;
  build)
    go build -buildvcs=false -o "${2:?Provide an output binary path}" .
    ;;
  *) echo "Usage: $0 [test | build OUTPUT]" >&2; exit 2 ;;
esac
