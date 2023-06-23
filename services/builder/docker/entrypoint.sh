#!/usr/bin/env bash

set -euo pipefail

cp -r /app /tmp/app

cd /tmp/app

echo $NIX_FLAKES | base64 -d > flake.nix
cp /tmp/builder/flake.lock /app/flake.lock

nix build \
    $NIX_OPTIONS \
    --extra-experimental-features nix-command \
    --extra-experimental-features flakes \
    --print-build-logs \
    .\#docker-image

cp -L result /app/docker-image.tar.gz
