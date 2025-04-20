package config

import (
	"fmt"
	"maps"
	"os"

	"govd/models"

	"gopkg.in/yaml.v3"
)

var extractorConfigs map[string]*models.ExtractorConfig

func LoadExtractorConfigs() error {
	extractorConfigs = make(map[string]*models.ExtractorConfig)
	configPath := "ext-cfg.yaml"

	_, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed reading config file: %w", err)
	}

	var rawConfig map[string]*models.ExtractorConfig

	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("failed parsing config file: %w", err)
	}
	maps.Copy(extractorConfigs, rawConfig)

	return nil
}

func GetExtractorConfig(codeName string) *models.ExtractorConfig {
	if config, exists := extractorConfigs[codeName]; exists {
		return config
	}
	return nil
}
