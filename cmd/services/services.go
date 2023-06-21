package services

import "github.com/urfave/cli/v2"

func Command() *cli.Command {
	return &cli.Command{ //nolint:exhaustruct
		Name:    "services",
		Aliases: []string{},
		Usage:   "Perform service operations",
		Subcommands: []*cli.Command{
			CommandBuild(),
		},
	}
}
