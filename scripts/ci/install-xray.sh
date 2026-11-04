#!/usr/bin/env bash
set -euo pipefail

# Test-only engine acquisition. Digests are from the v26.10.31 release's .dgst files.
case "$(uname -m)" in
  x86_64)
    archive=Xray-linux-64.zip
    digest=e4bd9119f50ae7c41658b1abd0679ae06f98bd864a2907ff33f2f2cf7081b214
    ;;
  aarch64|arm64)
    archive=Xray-linux-arm64-v8a.zip
    digest=1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d
    ;;
  *)
    printf 'Unsupported Xray test architecture\n' >&2
    exit 1
    ;;
esac

destination=${1:?Usage: bash scripts/ci/install-xray.sh ABSOLUTE_DESTINATION}
case "$destination" in
  /*) ;;
  *) printf 'Destination must be absolute\n' >&2; exit 1 ;;
esac

# Refuse an existing destination so another session's engine cannot be replaced.
mkdir -m 700 -- "$destination"
curl --fail --location --silent --show-error --retry 2 --connect-timeout 15 --max-time 180 \
  "https://github.com/XTLS/Xray-core/releases/download/v26.10.31/$archive" \
  --output "$destination/$archive"
printf '%s  %s\n' "$digest" "$destination/$archive" | sha256sum --check --status
python3 - "$destination/$archive" "$destination/xray" <<'PY'
import sys
import zipfile
from pathlib import Path

with zipfile.ZipFile(sys.argv[1]) as archive:
    binary = archive.read('xray')
target = Path(sys.argv[2])
target.write_bytes(binary)
target.chmod(0o700)
PY
"$destination/xray" version
