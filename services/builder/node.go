package builder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
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
                nodejs-version = final.nodejs_18;
                {{ else if eq .NodeVersion "20" }}
                nodejs-version = final.nodejs_20;
                {{ end }}
            })
          ];

        };

        buildInputs = with pkgs; [
          nodejs-version
          nodePackages.npm
          nodePackages.pnpm
          yarn
        ];

        nativeBuildInputs = with pkgs; [
        ];

        matchRegex = regex:
          args: path: type:
            (builtins.match "^/nix/store/.*-source/${regex}" path) != null;
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

                export PREV_PWD=$(pwd)
                export HOME=$(pwd)

                find .

                if [ -f package-lock.json ]; then
                    cd ${servicePath}
                    npm ci --frozen-lockfile
                elif [ -f yarn.lock ]; then
                    cd ${servicePath}
                    yarn install --frozen-lockfile
                else
                    echo "No lockfile found."
                    exit 1
                fi

                cd $PREV_PWD

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

                export HOME=$(pwd)
                ${buildCmd}

                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall

                mkdir -p $out/app
                cp -r * $out/app/
                cp -r ${node_modules}/node_modules $out/app/node_modules

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

func getNodeDependencyFiles(paths []string, cfgFolder string) []string {
	res := make([]string, 0, len(paths))
	if len(paths) == 0 {
		return []string{
			"package.json",
			"package-lock.json",
			"yarn.lock",
			path.Join(cfgFolder, "package.json"),
			path.Join(cfgFolder, "package-lock.json"),
			path.Join(cfgFolder, "yarn.lock"),
		}
	}

	for _, p := range paths {
		if strings.Contains(p, "package.json") ||
			strings.Contains(p, "package-lock.json") ||
			strings.Contains(p, "yarn.lock") {
			re := regexp.MustCompile(`\*{1,2}`)
			p = strings.ReplaceAll(p, ".", "\\.")
			res = append(res, re.ReplaceAllString(p, `.*`))
		}
	}

	return res
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
		NodeVersion:     nodeVersion,
		Env:             service.GetEnvironment(),
		Paths:           getPaths(service.GetImage().GetFiles(), relPath, "js"),
		DependencyFiles: getNodeDependencyFiles(service.GetImage().GetFiles(), relPath),
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
