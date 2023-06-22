package builder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nhost/be/services/mimir/model"
	"github.com/nhost/be/services/mimir/schema"
	"github.com/nhost/cli/clienv"
	"github.com/pelletier/go-toml/v2"
)

func unptr[T any](v *T) T { //nolint:ireturn
	if v == nil {
		return *new(T)
	}
	return *v
}

func loadServices(path string) ([]*model.ConfigService, error) {
	cfgf, err := os.Open(filepath.Join(path, "nhost-services.toml"))
	if err != nil {
		return nil, fmt.Errorf("could not open service configuration: %w", err)
	}
	defer cfgf.Close()

	b, err := io.ReadAll(cfgf)
	if err != nil {
		return nil, fmt.Errorf("could not read service configuration: %w", err)
	}

	var raw struct {
		Services []any `toml:"services"`
	}
	if err := toml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("could not parse service configuration: %w", err)
	}

	sch, err := schema.New()
	if err != nil {
		return nil, fmt.Errorf("could not create schema: %w", err)
	}

	services := make([]*model.ConfigService, len(raw.Services))
	for i, service := range raw.Services {
		svc, err := sch.FillService(service)
		if err != nil {
			return nil, fmt.Errorf("problem validating service configuration: %w", err)
		}
		services[i] = svc
	}

	return services, nil
}

func Build(ctx context.Context, cfgFolder, rootFolder string, version string, ce *clienv.CliEnv) error {
	svc, err := loadServices(cfgFolder)
	if err != nil {
		return err
	}

	for _, s := range svc {
		ce.Infoln("Service: %s", s.Name)
		switch unptr(s.GetImage().GetRuntime()) {
		case "nodejs":
			if err := buildNode(ctx, cfgFolder, rootFolder, s, version); err != nil {
				return err
			}
		case "python":
			ce.Infoln("Python")
		case "go":
			if err := buildGo(ctx, cfgFolder, rootFolder, s, version); err != nil {
				return err
			}
		default:
			ce.Infoln("Unknown")
		}
	}

	return nil
}

func getPaths(paths []string, svcPath string) []string {
	res := make([]string, len(paths))
	if len(paths) == 0 {
		return []string{
			path.Join(svcPath, ".*\\.js"),
		}
	}

	for i, p := range paths {
		re := regexp.MustCompile(`\*{1,2}`)
		p = strings.ReplaceAll(p, ".", "\\.")
		res[i] = re.ReplaceAllString(p, `.*`)
	}

	return res
}
