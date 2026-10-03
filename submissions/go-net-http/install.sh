#!/usr/bin/env bash
# Run once as root on a clean Ubuntu 24.04 x86_64: installs the toolchain build.sh needs.
set -euo pipefail
apt-get update -qq
apt-get install -y -qq build-essential curl
curl -fsSL https://go.dev/dl/go1.27.1.linux-amd64.tar.gz | tar -C /usr/local -xz
