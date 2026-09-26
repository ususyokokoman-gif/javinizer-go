package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_Scrape(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil))
	})

	t.Run("extracts scrape config", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority:      []string{"r18dev", "javdb"},
				UserAgent:     "test-agent",
				Referer:       "https://example.com",
				ScrapeActress: true,
			},
			Metadata: config.MetadataConfig{
				Translation: config.TranslationConfig{
					Enabled:        true,
					TargetLanguage: "ja",
				},
				ActressDatabase: config.ActressDatabaseConfig{
					Enabled: true,
				},
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Equal(t, []string{"r18dev", "javdb"}, result.ScrapersPriority)
		assert.True(t, result.TranslationEnabled)
		assert.Equal(t, "ja", result.TranslationTargetLang)
		assert.True(t, result.ScrapeActress)
	})

	t.Run("extracts Jev catalog validation config", func(t *testing.T) {
		t.Setenv("TYPESAFE_API_KEY", "")
		t.Setenv("JAVINIZER_JEV_CATALOG_THRESHOLD", "")
		t.Setenv("JAVINIZER_JEV_MODEL", "")
		t.Setenv("JAVINIZER_JEV_ENDPOINT", "")

		cfg := config.DefaultConfig(nil, nil)
		cfg.Metadata.CatalogIDValidation.Enabled = true
		cfg.Metadata.CatalogIDValidation.APIKey = "config-key"
		cfg.Metadata.CatalogIDValidation.Threshold = 0.84
		cfg.Metadata.CatalogIDValidation.Model = "jev-config"
		cfg.Metadata.CatalogIDValidation.Endpoint = "https://api.typesafe.ai/v1/systemone"

		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.True(t, result.JevCatalogEnabled)
		assert.Equal(t, "config-key", result.JevCatalogAPIKey)
		assert.Equal(t, 0.84, result.JevCatalogThreshold)
		assert.Equal(t, "jev-config", result.JevCatalogModel)
		assert.Equal(t, "https://api.typesafe.ai/v1/systemone", result.JevCatalogEndpoint)
	})

	t.Run("TYPESAFE_API_KEY overrides config and enables Jev gate", func(t *testing.T) {
		t.Setenv("TYPESAFE_API_KEY", "env-key")
		t.Setenv("JAVINIZER_JEV_CATALOG_THRESHOLD", "0.80")
		t.Setenv("JAVINIZER_JEV_MODEL", "jev-env")
		t.Setenv("JAVINIZER_JEV_ENDPOINT", "https://api.typesafe.ai/v1/systemone")

		cfg := config.DefaultConfig(nil, nil)
		cfg.Metadata.CatalogIDValidation.Enabled = false
		cfg.Metadata.CatalogIDValidation.APIKey = "config-key"
		cfg.Metadata.CatalogIDValidation.Threshold = 0.91
		cfg.Metadata.CatalogIDValidation.Model = "jev-config"

		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.True(t, result.JevCatalogEnabled)
		assert.Equal(t, "env-key", result.JevCatalogAPIKey)
		assert.Equal(t, 0.80, result.JevCatalogThreshold)
		assert.Equal(t, "jev-env", result.JevCatalogModel)
	})

	t.Run("translation disabled omits hash", func(t *testing.T) {
		cfg := &config.Config{
			Metadata: config.MetadataConfig{
				Translation: config.TranslationConfig{
					Enabled: false,
				},
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Empty(t, result.TranslationSettingsHash)
	})
}
