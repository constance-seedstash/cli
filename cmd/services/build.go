package services

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/nhost/cli/clienv"
	"github.com/nhost/cli/services/builder"
	"github.com/urfave/cli/v2"
)

const (
	flagConfigFolder = "config-folder"
	flagRootFolder   = "root-folder"
	flagVersion      = "version"
)

func CommandBuild() *cli.Command {
	defaultRootFolder, err := getRootFolder(".")
	if err != nil {
		defaultRootFolder = "."
	}

	return &cli.Command{ //nolint:exhaustruct
		Name:    "build",
		Aliases: []string{},
		Usage:   "Create default configuration and secrets",
		Action:  commandBuild,
		Flags: []cli.Flag{
			&cli.StringFlag{ //nolint:exhaustruct
				Name:    flagConfigFolder,
				Usage:   "Path to the folder where the service configuration is located",
				EnvVars: []string{"NHOST_SERVICE_CONFIG_FOLDER"},
				Value:   ".",
			},
			&cli.StringFlag{ //nolint:exhaustruct
				Name:    flagRootFolder,
				Usage:   "Path to the root folder of the service",
				EnvVars: []string{"NHOST_SERVICE_ROOT_FOLDER"},
				Value:   defaultRootFolder,
			},
			&cli.StringFlag{ //nolint:exhaustruct
				Name:    flagVersion,
				Usage:   "Version of the service",
				Value:   "0.0.0-dev",
				EnvVars: []string{"NHOST_SERVICE_VERSION"},
			},
		},
	}
}

func getRootFolder(path string) (string, error) {
	repo, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: false,
	})
	if err != nil {
		return "", fmt.Errorf("could not open git repository: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("could not open git worktree: %w", err)
	}

	return wt.Filesystem.Root(), nil
}

func commandBuild(cCtx *cli.Context) error {
	ce := clienv.FromCLI(cCtx)

	cfgFolder := cCtx.String(flagConfigFolder)
	rootFolder := cCtx.String(flagRootFolder)

	ce.Infoln("Building service at %s with rootFolder %s", cfgFolder, rootFolder)

	return builder.Build(cCtx.Context, cfgFolder, rootFolder, cCtx.String(flagVersion), ce) //nolint:wrapcheck
}
