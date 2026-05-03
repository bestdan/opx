#!/usr/bin/env bash
set -euo pipefail

make build && make test && make lint && sudo mv opx /usr/local/bin/
