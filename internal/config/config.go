package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string

	// EIS
	EISToken      string // UUID / токен пользователя (index.sender)
	EISEndpoint   string
	EISProfile    string // mis | ip | le | org
	EISOrgRegion  string // КЛАДР регион, напр. "77"
	EISSubsystem  string // PRIZ по умолчанию
	EISDocTypes   []string
	EISMode       string // PROD | TEST
	EISTLSCert   string
	EISTLSKey    string
	EISCACert     string
	EISSkipVerify bool
	EISLookback   time.Duration // окно выборки (по умолчанию 30m)
	EISInterval   time.Duration // цикл опроса (по умолчанию 29m)
	EISTimezone   int           // смещение часового пояса для periodInfo

	HTTPAddr string
}

func Load() (Config, error) {
	profile := env("EIS_PROFILE", "mis")
	cfg := Config{
		DatabaseURL:   env("DATABASE_URL", "postgres://zakupki:zakupki@localhost:5432/zakupki?sslmode=disable"),
		EISToken:      env("EIS_TOKEN", ""),
		EISEndpoint:   env("EIS_ENDPOINT", defaultEndpoint(profile)),
		EISProfile:    profile,
		EISOrgRegion:  env("EIS_ORG_REGION", "77"),
		EISSubsystem:  env("EIS_SUBSYSTEM", "PRIZ"),
		EISDocTypes:   splitCSV(env("EIS_DOC_TYPES", "epNotificationEF2020,fcsNotificationEF,epNotificationEOK2020,fcsNotificationEP")),
		EISMode:       env("EIS_MODE", "PROD"),
		EISTLSCert:   env("EIS_TLS_CERT", ""),
		EISTLSKey:    env("EIS_TLS_KEY", ""),
		EISCACert:     env("EIS_CA_CERT", ""),
		EISSkipVerify: envBool("EIS_TLS_SKIP_VERIFY", false),
		EISLookback:   envDuration("EIS_LOOKBACK", 30*time.Minute),
		EISInterval:   envDuration("EIS_INTERVAL", 29*time.Minute),
		EISTimezone:   envInt("EIS_TIMEZONE_OFFSET", 3),
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
	}
	if cfg.EISToken == "" {
		return cfg, fmt.Errorf("EIS_TOKEN (UUID пользователя) обязателен")
	}
	return cfg, nil
}

func defaultEndpoint(profile string) string {
	base := "https://int.zakupki.gov.ru/eis-integration"
	switch strings.ToLower(profile) {
	case "ip":
		return base + "/services/getDocsIP"
	case "le":
		return base + "/services/getDocsLE"
	case "org":
		return base + "/services/getDocsOrganization"
	case "ris":
		return base + "/services-ris/getDocsRis"
	default:
		return base + "/services-mis/getDocsMis"
	}
}

func LoadAPI() Config {
	return Config{
		DatabaseURL: env("DATABASE_URL", "postgres://zakupki:zakupki@localhost:5432/zakupki?sslmode=disable"),
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
