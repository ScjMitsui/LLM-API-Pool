package main

import (
	"os"
	"sync"
	"gopkg.in/yaml.v3"
)

type PoolTarget struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	Model    string `yaml:"model" json:"model"`
}

type Config struct {
	Server       ServerConfig                 `yaml:"server"`
	Endpoints    []EndpointConfig             `yaml:"endpoints"`
	Pool         PoolConfig                   `yaml:"pool"`
	ModelAliases map[string][]PoolTarget      `yaml:"model_aliases"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type EndpointConfig struct {
	Name     string   `yaml:"name"`
	APIBase  string   `yaml:"api_base"`
	APIKey   string   `yaml:"api_key"`
	Enabled  bool     `yaml:"enabled"`
	Models   []string `yaml:"models,omitempty"`
}

type PoolConfig struct {
	Strategy            string `yaml:"strategy"`
	MaxRetries          int    `yaml:"max_retries"`
	HealthCheckCooldown int    `yaml:"health_check_cooldown"`
	ModelCacheTTL       int    `yaml:"model_cache_ttl,omitempty"`
}

var (
	AppConfig Config
	cfgLock   sync.RWMutex
)

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var newCfg Config
	if err := yaml.Unmarshal(data, &newCfg); err != nil {
		return err
	}

	cfgLock.Lock()
	defer cfgLock.Unlock()
	AppConfig = newCfg
	
	if AppConfig.Server.Port == 0 {
		AppConfig.Server.Port = 5066
	}
	if AppConfig.Pool.Strategy == "" {
		AppConfig.Pool.Strategy = "round-robin"
	}
	if AppConfig.Pool.MaxRetries == 0 {
		AppConfig.Pool.MaxRetries = 2
	}
	if AppConfig.Pool.HealthCheckCooldown == 0 {
		AppConfig.Pool.HealthCheckCooldown = 60
	}
	if AppConfig.Pool.ModelCacheTTL == 0 {
		AppConfig.Pool.ModelCacheTTL = 300
	}
	return nil
}

func SaveConfig(path string) error {
	cfgLock.RLock()
	data, err := yaml.Marshal(&AppConfig)
	cfgLock.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
} 
