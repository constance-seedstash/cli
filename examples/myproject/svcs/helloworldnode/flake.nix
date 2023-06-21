{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nix-filter.url = "github:numtide/nix-filter";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, nix-filter, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        name = "helloworldnode";
        version = "5C5E7D53-45EB-4BA4-AB3D-1636688B69D7";
        buildCmd = "";

        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (final: prev: { })
          ];
        };

        src = ./.;

        buildInputs = with pkgs; [
          nodejs-slim
        ];

        nativeBuildInputs = with pkgs; [
          nodePackages.npm
          nodePackages.pnpm
          yarn
        ];
      in
      {
        packages = flake-utils.lib.flattenTree
          rec {
            node_modules = pkgs.stdenv.mkDerivation {
              inherit version buildInputs nativeBuildInputs;

              name = "${name}-node_modules";
              src = nix-filter.lib.filter {
                root = ./.;
                include = with nix-filter.lib;[
                  "package.json"
                  "package-lock.json"
                ];
              };

              buildPhase = ''
                runHook preBuild

                export HOME=$(pwd)
                ls -la
                if [ -f package-lock.json ]; then
                    npm ci --frozen-lockfile
                fi

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/
                cp -rv node_modules $out/node_modules

                runHook postInstall
              '';
            };

            nodeapp = pkgs.stdenv.mkDerivation {
              inherit name version src buildInputs nativeBuildInputs;
              buildPhase = ''
                runHook preBuild

                export HOME=$(pwd)

                ${buildCmd}

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/app
                cp -rv * $out/app
                cp -rv ${node_modules}/node_modules $out/app/node_modules

                runHook postInstall
              '';
            };

            # entrypoint = pkgs.writeScriptBin "entrypoint" ''
            #   #!/bin/sh
            #   set -euo pipefail
            #   ${runCmd}
            # '';

            docker-image = pkgs.dockerTools.buildLayeredImage {
              name = "helloworldnode";
              tag = "latest";
              created = "now";

              contents = with pkgs; [
                cacert
                nodeapp
              ] ++ buildInputs;

              config = {
                workingDir = "/app";
              };
            };
          };
      }
    );
}
