package telemetry

import (
	"fmt"

	"github.com/caarlos0/env"
)

type Config struct {
	ServiceName              string `env:"SERVICE_NAME" envDefault:"kronos-node"`
	ServiceVersion           string `env:"SERVICE_VERSION" envDefault:"0.0.1"`
	OtelExporterOtlpEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"http://localhost:4317"`
	PrometheusURL            string `env:"PROMETHEUS_URL" envDefault:"http://localhost:9090"`
	PrometheusBearerToken    string `env:"PROMETHEUS_BEARER_TOKEN"`
	PrometheusBasicAuthUser  string `env:"PROMETHEUS_BASIC_AUTH_USER"`
	PrometheusBasicAuthPass  string `env:"PROMETHEUS_BASIC_AUTH_PASS"`
	Enabled                  bool   `env:"TELEMETRY_ENABLED" envDefault:"true"`
}

func NewConfigFromEnv() (Config, error) {
	telem := Config{}

	if err := env.Parse(&telem); err != nil {
		return Config{}, fmt.Errorf("failed to parse telemetry config: %w", err)
	}

	return telem, nil
}
