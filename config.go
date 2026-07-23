package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type BackoffConfig struct {
	Base     time.Duration `yaml:"base"`
	Factor   float64       `yaml:"factor"`
	Max      time.Duration `yaml:"max"`
	MaxTries int           `yaml:"max_tries"`
}

type ProcessConfig struct {
	Name          string            `yaml:"name"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Env           map[string]string `yaml:"env"`
	WorkingDir    string            `yaml:"working_dir"`
	RestartPolicy string            `yaml:"restart_policy"`
	Backoff       *BackoffConfig    `yaml:"backoff"`
	StopSignal    string            `yaml:"stop_signal"`
	StopWait      time.Duration     `yaml:"stop_wait"`
}

type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

type Config struct {
	HTTPAPI   HTTPConfig      `yaml:"http_api"`
	Processes []ProcessConfig `yaml:"processes"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("analizar YAML: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuración inválida: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("configuración nula")
	}
	if c.HTTPAPI.Port < 0 || c.HTTPAPI.Port > 65535 {
		return fmt.Errorf("puerto HTTP fuera de rango: %d", c.HTTPAPI.Port)
	}
	seen := make(map[string]struct{}, len(c.Processes))
	for i := range c.Processes {
		p := &c.Processes[i]
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("processes[%d]: nombre vacío", i)
		}
		if _, exists := seen[p.Name]; exists {
			return fmt.Errorf("nombre duplicado: %q", p.Name)
		}
		seen[p.Name] = struct{}{}
		if strings.TrimSpace(p.Command) == "" {
			return fmt.Errorf("proceso %q: comando vacío", p.Name)
		}
		if p.RestartPolicy == "" {
			p.RestartPolicy = "never"
		}
		switch p.RestartPolicy {
		case "always", "on-failure", "never":
		default:
			return fmt.Errorf("proceso %q: política inválida %q", p.Name, p.RestartPolicy)
		}
		if p.StopWait < 0 {
			return fmt.Errorf("proceso %q: stop_wait negativo", p.Name)
		}
		if p.Backoff != nil {
			b := p.Backoff
			if b.Base < 0 || b.Max < 0 {
				return fmt.Errorf("proceso %q: duración de backoff negativa", p.Name)
			}
			if b.Factor < 0 || (b.Factor > 0 && b.Factor <= 1) {
				return fmt.Errorf("proceso %q: factor de backoff debe ser mayor que 1", p.Name)
			}
			if b.MaxTries < -1 {
				return fmt.Errorf("proceso %q: max_tries debe ser -1 o no negativo", p.Name)
			}
			if b.Base > 0 && b.Max > 0 && b.Max < b.Base {
				return fmt.Errorf("proceso %q: máximo de backoff menor que la base", p.Name)
			}
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.HTTPAPI.Host == "" {
		c.HTTPAPI.Host = "127.0.0.1"
	}
	if c.HTTPAPI.Port == 0 {
		c.HTTPAPI.Port = 8080
	}
	for i := range c.Processes {
		if c.Processes[i].RestartPolicy == "" {
			c.Processes[i].RestartPolicy = "never"
		}
	}
}
