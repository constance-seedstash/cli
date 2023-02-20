package compose

import (
	"fmt"

	"github.com/compose-spec/compose-go/types"
	"github.com/nhost/cli/internal/generichelper"
)

func (c Config) storageServiceEnvs(apiRootPrefix string) env {
	minioEnv := c.minioServiceEnvs()
	hasuraConf := c.nhostConfig.Hasura()

	e := env{
		"DEBUG":                       "true",
		"BIND":                        ":8576",
		"PUBLIC_URL":                  c.storageEnvPublicURL(),
		"API_ROOT_PREFIX":             apiRootPrefix,
		"POSTGRES_MIGRATIONS":         "1",
		"HASURA_METADATA":             "1",
		"HASURA_ENDPOINT":             "http://graphql:8080/v1",
		"HASURA_GRAPHQL_ADMIN_SECRET": hasuraConf.GetAdminSecret(),
		"S3_ACCESS_KEY":               minioEnv[envMinioRootUser],
		"S3_SECRET_KEY":               minioEnv[envMinioRootPassword],
		"S3_ENDPOINT":                 "http://minio:9000",
		"S3_BUCKET":                   "nhost",
		"HASURA_GRAPHQL_JWT_SECRET":   c.graphqlJwtSecret(),
		"POSTGRES_MIGRATIONS_SOURCE":  fmt.Sprintf("%s?sslmode=disable", c.postgresConnectionStringForUser("nhost_storage_admin")),
	}.merge(c.nhostSystemEnvs(), c.globalEnvs)

	return e
}

// deprecated
// We need to keep this for backward compatibility with deprecated backend services where "API_ROOT_PREFIX" env differs
func (c Config) httpStorageService() *types.ServiceConfig {
	httpLabels := makeTraefikServiceLabels(
		"http-"+SvcStorage,
		storagePort,
		withPathPrefix("/v1/storage"),
	)

	return &types.ServiceConfig{
		Name:        "http-" + SvcStorage,
		Restart:     types.RestartPolicyAlways,
		Image:       "nhost/hasura-storage:" + generichelper.DerefPtr(c.nhostConfig.Storage().GetVersion()),
		Environment: c.storageServiceEnvs("/v1/storage").dockerServiceConfigEnv(),
		Labels:      httpLabels.AsMap(),
		Command:     []string{"serve"},
	}
}

func (c Config) storageService() *types.ServiceConfig {
	sslLabels := makeTraefikServiceLabels(
		SvcStorage,
		storagePort,
		withTLS(),
		withPathPrefix("/v1"),
		withHost(HostLocalStorageNhostRun),
	)

	return &types.ServiceConfig{
		Name:        SvcStorage,
		Restart:     types.RestartPolicyAlways,
		Image:       "nhost/hasura-storage:" + generichelper.DerefPtr(c.nhostConfig.Storage().GetVersion()),
		Environment: c.storageServiceEnvs("").dockerServiceConfigEnv(),
		Labels:      sslLabels.AsMap(),
		Command:     []string{"serve"},
	}
}