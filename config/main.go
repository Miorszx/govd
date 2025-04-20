package config

import (
	"fmt"
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
		return fmt.Errorf("errore nella lettura del file di configurazione: %w", err)
	}

	var rawConfig map[string]*models.ExtractorConfig

	if err := yaml.Unmarshal(data, &rawConfig); err != nil {
		return fmt.Errorf("errore nella decodifica del file YAML: %w", err)
	}
	for codeName, config := range rawConfig {
		extractorConfigs[codeName] = config
	}

	return nil
}

func GetExtractorConfig(codeName string) *models.ExtractorConfig {
	if config, exists := extractorConfigs[codeName]; exists {
		return config
	}
	return nil
}
