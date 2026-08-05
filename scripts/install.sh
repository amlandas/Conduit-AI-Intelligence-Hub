#!/bin/bash
#
# Conduit v1 installer — RETIRED (August 2026)
#
# This script used to install the v1 stack: a daemon, Docker/Podman containers
# (Qdrant, FalkorDB) and Ollama models. That architecture is retired and this
# installer no longer installs anything.
#
# Conduit 2.0 is a single binary: no daemon, no containers, no Ollama required.

set -euo pipefail

cat >&2 <<'MSG'
==============================================================
  Conduit v1 is retired — this installer has been disabled.
==============================================================

  Conduit 2.0 is one binary. No daemon, no Docker/Podman,
  no Ollama, no containers.

  Install v2:

    git clone -b v2 https://github.com/amlandas/Conduit-AI-Intelligence-Hub
    cd Conduit-AI-Intelligence-Hub
    ./scripts/install.sh

  If this machine has an old v1 install (daemon, containers),
  remove it first with the hardened teardown script:

    ./scripts/remove-v1.sh --dry-run    # preview
    ./scripts/remove-v1.sh --yes        # remove

  Docs: https://github.com/amlandas/Conduit-AI-Intelligence-Hub/blob/v2/docs/INSTALL_V2.md

MSG
exit 1
