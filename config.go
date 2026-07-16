package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ProcessConfig define la configuración de un proceso individual a supervisar.
type ProcessConfig struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Env           map[string]string `yaml:"env"`
	WorkingDir    string            `yaml:"working_dir"`
	RestartPolicy string            `yaml:"restart_policy"` // always, on-failure, never
}

// Config define el archivo raíz que contiene múltiples procesos.
type Config struct {
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
