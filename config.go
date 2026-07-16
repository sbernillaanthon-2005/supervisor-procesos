package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// BackoffConfig define las reglas de reinicio exponencial.
type BackoffConfig struct {
	Base     time.Duration `yaml:"base"`
	Factor   float64       `yaml:"factor"`
	Max      time.Duration `yaml:"max"`
	MaxTries int           `yaml:"max_tries"`
}

// ProcessConfig define la configuración de un proceso individual a supervisar.
type ProcessConfig struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Env           map[string]string `yaml:"env"`
	WorkingDir    string            `yaml:"working_dir"`
	RestartPolicy string            `yaml:"restart_policy"` // always, on-failure, never
	Backoff       *BackoffConfig    `yaml:"backoff"`
	StopSignal    string            `yaml:"stop_signal"`
	StopWait      time.Duration     `yaml:"stop_wait"`
}

// HTTPConfig define la configuración del servidor HTTP integrado (H5).
type HTTPConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// Config define el archivo raíz que contiene múltiples procesos y la API HTTP.
type Config struct {
	HTTPAPI   HTTPConfig      `yaml:"http_api"`
	Processes []ProcessConfig `yaml:"processes"`
}

// LoadConfig lee y parsea el archivo YAML de configuración.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error leyendo archivo %s: %w", path, err)
	}

	var cfg Config
	// yaml.Unmarshal parsea los datos según los tags `yaml` de las structs.
	// Si falta un campo en el YAML, el campo en Go retendrá su valor por defecto (Zero Value).
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error parseando YAML: %w", err)
	}

	return &cfg, nil
}
