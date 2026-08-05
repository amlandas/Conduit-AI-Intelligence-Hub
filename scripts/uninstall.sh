#!/bin/bash
#
# Conduit v1 uninstaller — RETIRED (August 2026)
#
# Superseded by scripts/remove-v1.sh on the v2 branch, which removes the
# complete v1 stack (daemon services, containers, binaries) with hardened,
# adversarially-reviewed data-safety guards this script never had.

set -euo pipefail

cat >&2 <<'MSG'
==============================================================
  This v1 uninstaller has been retired.
==============================================================

  Use the hardened v1 teardown script from the v2 branch instead:

    git clone -b v2 https://github.com/amlandas/Conduit-AI-Intelligence-Hub
    cd Conduit-AI-Intelligence-Hub
    ./scripts/remove-v1.sh --dry-run    # preview what would be removed
    ./scripts/remove-v1.sh --yes        # remove the v1 stack

  Your knowledge-base data is never touched without an explicit
  --purge-data flag.

MSG
exit 1
