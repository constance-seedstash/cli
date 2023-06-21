package builder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/nhost/be/services/mimir/model"
)

const defaultNodeversion = "18"

const nodeTemplate = `{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nix-filter.url = "github:numtide/nix-filter";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, nix-filter, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        name = "{{ .Name }}";
        version = "{{ .Version }}";
        buildCmd = "{{ .BuildCmd }}";
        servicePath = "{{ .ServicePath }}";

        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (final: prev: {
                {{ if eq .NodeVersion "18" }}
                nodejs-version = final.nodejs-slim;
                {{ else if eq .NodeVersion "20" }}
                nodejs-version = final.nodejs-slim_20;
                {{ end }}
            })
          ];

        };

        src = ./.;

        buildInputs = with pkgs; [
          nodejs-version
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
                  "${servicePath}/package.json"
                  "${servicePath}/package-lock.json"
                ];
              };

              buildPhase = ''
                runHook preBuild

                cd ${servicePath}
                export HOME=$(pwd)

                if [ -f package-lock.json ]; then
                    npm ci --frozen-lockfile
                fi

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                find .
                mkdir -p $out/
                cp -r node_modules $out/node_modules

                runHook postInstall
              '';
            };

            nodeapp = pkgs.stdenv.mkDerivation {
              inherit name version src buildInputs nativeBuildInputs;
              buildPhase = ''
                runHook preBuild

                cd ${servicePath}
                export HOME=$(pwd)

                ${buildCmd}

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                find .

                mkdir -p $out/app
                cp -r * $out/app/
                cp -r ${node_modules}/node_modules $out/app/node_modules

                runHook postInstall
              '';
            };

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
`

func discoverNodeVersion(paths ...string) (string, error) {
	var packageJSON struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	for _, path := range paths {
		f, err := os.Open(filepath.Join(path, "package.json"))
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			return "", fmt.Errorf("could not open package.json: %w", err)
		}
		defer f.Close()

		fmt.Println("package.json: ", f.Name())

		if err := json.NewDecoder(f).Decode(&packageJSON); err != nil {
			return "", fmt.Errorf("could not decode package.json: %w", err)
		}

		if packageJSON.Engines.Node != "" {
			c, err := semver.NewConstraint(packageJSON.Engines.Node)
			if err != nil {
				return "", fmt.Errorf("could not parse node version: %w", err)
			}
			switch {
			case c.Check(semver.MustParse("18.16.0")):
				return "18", nil
			case c.Check(semver.MustParse("20.2.0")):
				return "20", nil
			default:
				return "", fmt.Errorf("unsupported node version: %s", packageJSON.Engines.Node)
			}
		}
	}

	return defaultNodeversion, nil
}

func buildNode(
	ctx context.Context,
	cfgFolder,
	rootFolder string,
	service *model.ConfigService,
	version string,
) error {
	tmpl := template.Must(template.New("flake.nix").Parse(nodeTemplate))

	nodeVersion, err := discoverNodeVersion(cfgFolder, rootFolder)
	if err != nil {
		return err
	}

	data := struct {
		Name        string
		Version     string
		BuildCmd    string
		ServicePath string
		NodeVersion string
		Env         []*model.ConfigEnvironmentVariable
	}{
		Name:        service.Name,
		Version:     version,
		ServicePath: cfgFolder,
		BuildCmd:    strings.Join(service.GetImage().BuildCommand, " "),
		NodeVersion: nodeVersion,
		Env:         service.GetEnvironment(),
	}

	strbuilder := &strings.Builder{}
	if err := tmpl.Execute(strbuilder, data); err != nil {
		return fmt.Errorf("could not execute template: %w", err)
	}

	base64str := base64.StdEncoding.EncodeToString([]byte(strbuilder.String()))

	cmd := exec.CommandContext( //nolint:gosec
		ctx,
		"docker",
		"run",
		"--rm",
		"-e", "NIX_FLAKES="+base64str,
		"-e", "NIX_OPTIONS=",
		"-v", rootFolder+":/app",
		"--mount", "type=volume,source=nhost-builder-nix,target=/nix",
		"builders:latest",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not build with docker: %w", err)
	}

	cmd = exec.CommandContext( //nolint:gosec
		ctx,
		"docker",
		"load",
		"-i",
		filepath.Join(rootFolder, "docker-image.tar.gz"),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not run docker load: %w", err)
	}

	if err := os.Remove(filepath.Join(rootFolder, "docker-image.tar.gz")); err != nil {
		return fmt.Errorf("could not remove docker-image.tar.gz: %w", err)
	}

	return nil
}
