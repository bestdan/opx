 #!/usr/bin/env bash

set -euo pipefail

args=()
  while IFS= read -r line; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    
    args+=(--env "$line")
  done < .env.example

  eval "$(opx "${args[@]}")"
  echo "CREDS=${#CREDS} KEYS=${#KEYS}"

  echo "$CREDS" | head -c 4; echo