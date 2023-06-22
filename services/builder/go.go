package builder

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
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
            (final: prev: { })
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
            vendor = pkgs.stdenv.mkDerivation {
              inherit version buildInputs nativeBuildInputs;

              name = "${name}-vendor";
              src = nix-filter.lib.filter {
                root = ./.;
                include = with nix-filter.lib;[
                  {{range .DependencyFiles}}
                  "{{.}}"
                  {{end}}
                ];
              };

              buildPhase = ''
                runHook preBuild

                find -type d -name vendor | grep vendor
                if [ $? -neq 0 ]; then
                  export PREV_PWD=$(pwd)
                  cd ${servicePath}
                  export HOME=$(pwd)

                  go mod vendor
                fi

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/
                cp -r node_modules $out/node_modules

                runHook postInstall
              '';
            };

            nodeapp = pkgs.stdenv.mkDerivation {
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

                export PREV_PWD=$(pwd)
                cd ${servicePath}
                export HOME=$(pwd)
                ${buildCmd}

                cd $PREV_PWD

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/app
                cp -r * $out/app/
                cp -r ${node_modules}/node_modules $out/app/${servicePath}/node_modules

                runHook postInstall
              '';
            };

            docker-image = pkgs.dockerTools.buildLayeredImage {
              inherit name;
              tag = version;
              created = "now";

              contents = with pkgs; [
                cacert
                nodeapp
              ] ++ buildInputs;

              config = {
                workingDir = "/app/${servicePath}";
              };
            };
          };
      }
    );
}
`

func getGoDependencyFiles(paths []string, cfgFolder string) []string {
	res := make([]string, 0, len(paths))
	if len(paths) == 0 {
		return []string{
			path.Join(cfgFolder, "go.mod"),
			path.Join(cfgFolder, "go.sum"),
			path.Join(cfgFolder, "vendor"),
		}
	}

	for _, p := range paths {
		if strings.Contains(p, "go.mod") ||
			strings.Contains(p, "go.sum") ||
			strings.Contains(p, "vendor") {
			re := regexp.MustCompile(`\*{1,2}`)
			p = strings.ReplaceAll(p, ".", "\\.")
			res = append(res, re.ReplaceAllString(p, `.*`))
		}
	}

	return res
}

func buildGo(
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
		ServicePath:     cfgFolder,
		BuildCmd:        strings.Join(service.GetImage().BuildCommand, " "),
		NodeVersion:     nodeVersion,
		Env:             service.GetEnvironment(),
		Paths:           getPaths(service.GetImage().GetFiles(), cfgFolder),
		DependencyFiles: getGoDependencyFiles(service.GetImage().GetFiles(), cfgFolder),
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
