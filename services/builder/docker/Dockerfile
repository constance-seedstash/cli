FROM nixos/nix:2.16.1

COPY flake.lock /tmp/builder/flake.lock
COPY services/builder/docker/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
