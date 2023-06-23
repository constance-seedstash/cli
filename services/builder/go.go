package builder

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/nhost/be/services/mimir/model"
)

const goTemplate = `{
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
            })
          ];

        };

        buildInputs = with pkgs; [
        ];

        nativeBuildInputs = with pkgs; [
          go
        ];

        matchRegex = regex:
          args: path: type:
            (builtins.match "^/nix/store/.*-source/${regex}" path) != null;
      in
      {
        packages = flake-utils.lib.flattenTree
          rec {
            dependencies = pkgs.stdenv.mkDerivation {
              inherit version buildInputs nativeBuildInputs;

              name = "${name}-dependencies";
              src = nix-filter.lib.filter {
                root = ./.;
                include = with nix-filter.lib;[
                  isDirectory
                  {{range .DependencyFiles}}
                  "{{.}}"
                  {{end}}
                ];
              };

              {{range .Env}}
              {{.Name}} = "{{.Value}}";
              {{end}}

              buildPhase = ''
                runHook preBuild

                export GOCACHE=$TMPDIR/.cache/go-build
                export GOMODCACHE="$TMPDIR/.cache/mod"
                export GOPATH="$TMPDIR/.cache/gopath"

                if [ ! -d vendor ]; then
                    go mod vendor
                fi

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/
                cp -r vendor $out/

                runHook postInstall
              '';
            };

            app = pkgs.stdenv.mkDerivation {
              inherit name version buildInputs nativeBuildInputs;

              src = nix-filter.lib.filter {
                root = ./.;
                include = with nix-filter.lib;[
                  isDirectory
                  {{range .DependencyFiles}}
                  "{{.}}"
                  {{end}}
                  {{range .Paths}}
                  (matchRegex "{{.}}$")
                  {{end}}
                ];
              };

              {{range .Env}}
              {{.Name}} = "{{.Value}}";
              {{end}}

              buildPhase = ''
                runHook preBuild

                export GOCACHE=$TMPDIR/.cache/go-build
                export GOMODCACHE="$TMPDIR/.cache/mod"
                export GOPATH="$TMPDIR/.cache/gopath"

                rm -rf vendor
                cp -r ${dependencies}/vendor vendor
                go build -o ${name} ./${servicePath}/

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/bin
                cp -r ${name} $out/bin/

                runHook postInstall
              '';
            };

            docker-image = pkgs.dockerTools.buildLayeredImage {
              inherit name;
              tag = version;
              created = "now";

              contents = with pkgs; [
                cacert
                busybox
                app
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

func getGoDependencyFiles(paths []string) []string {
	return []string{
		"go\\.mod",
		"go\\.sum",
		"vendor",
	}
}

func buildGo(
	ctx context.Context,
	cfgFolder,
	rootFolder string,
	service *model.ConfigService,
	version string,
) error {
	tmpl := template.Must(template.New("flake.nix").Parse(goTemplate))

	relPath, err := filepath.Rel(rootFolder, cfgFolder)
	if err != nil {
		return fmt.Errorf("could not get relative path: %w", err)
	}

	data := struct {
		Name            string
		Version         string
		BuildCmd        string
		ServicePath     string
		NodeVersion     string
		DependencyFiles []string
		Paths           []string
		Env             []*model.ConfigEnvironmentVariable
	}{
		Name:            service.Name,
		Version:         version,
		ServicePath:     relPath,
		BuildCmd:        strings.Join(service.GetImage().BuildCommand, " "),
		Env:             service.GetEnvironment(),
		Paths:           getPaths(service.GetImage().GetFiles(), relPath, "go"),
		DependencyFiles: getGoDependencyFiles(service.GetImage().GetFiles()),
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
