package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	// 1. Setup: Crear un archivo YAML temporal para la prueba
	yamlContent := []byte(`
processes:
  - name: test_proc
    command: "python"
    args: ["-c", "print('hola')"]
    env:
      FOO: "bar"
    working_dir: "/tmp"
    restart_policy: always
  - name: minimal_proc
    command: "echo"
`)
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("no se pudo crear archivo temporal: %v", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean-up al final de la prueba

	if _, err := tmpFile.Write(yamlContent); err != nil {
		t.Fatalf("no se pudo escribir archivo temporal: %v", err)
	}
	tmpFile.Close()

	// 2. Act: Llamar a la función que queremos probar
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig retornó error inesperado: %v", err)
	}

	// 3. Assert: Verificar que los datos parseados sean correctos
	if len(cfg.Processes) != 2 {
		t.Fatalf("se esperaban 2 procesos, se obtuvieron %d", len(cfg.Processes))
	}

	// Comprobamos el proceso completo
	proc1 := cfg.Processes[0]
	if proc1.Name != "test_proc" {
		t.Errorf("nombre incorrecto, esperado 'test_proc', obtenido '%s'", proc1.Name)
	}
	if proc1.Env["FOO"] != "bar" {
		t.Errorf("env FOO incorrecto, esperado 'bar', obtenido '%s'", proc1.Env["FOO"])
	}
	if proc1.RestartPolicy != "always" {
		t.Errorf("restart_policy incorrecto, esperado 'always', obtenido '%s'", proc1.RestartPolicy)
	}

	// Comprobamos el comportamiento de campos omitidos (Zero Values)
	proc2 := cfg.Processes[1]
	if proc2.Name != "minimal_proc" {
		t.Errorf("nombre incorrecto, esperado 'minimal_proc', obtenido '%s'", proc2.Name)
	}
	if proc2.WorkingDir != "" {
		t.Errorf("working_dir debería estar vacío por defecto, se obtuvo '%s'", proc2.WorkingDir)
	}
	if len(proc2.Args) != 0 {
		t.Errorf("args debería estar vacío, se obtuvieron %d elementos", len(proc2.Args))
	}
}

func TestConfigValidate(t *testing.T) {
	valid := ProcessConfig{Name: "worker", Command: "go", RestartPolicy: "never"}
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"nombre vacío", Config{Processes: []ProcessConfig{{Command: "go"}}}, "nombre vacío"},
		{"duplicado", Config{Processes: []ProcessConfig{valid, valid}}, "duplicado"},
		{"comando vacío", Config{Processes: []ProcessConfig{{Name: "x"}}}, "comando vacío"},
		{"política", Config{Processes: []ProcessConfig{{Name: "x", Command: "go", RestartPolicy: "sometimes"}}}, "política inválida"},
		{"stop negativo", Config{Processes: []ProcessConfig{{Name: "x", Command: "go", StopWait: -time.Second}}}, "negativo"},
		{"factor", Config{Processes: []ProcessConfig{{Name: "x", Command: "go", Backoff: &BackoffConfig{Factor: 1}}}}, "factor"},
		{"puerto", Config{HTTPAPI: HTTPConfig{Port: 70000}, Processes: []ProcessConfig{valid}}, "puerto"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v; se esperaba error con %q", err, tc.want)
			}
		})
	}
}

func TestLoadConfigAppliesHTTPDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("http_api:\n  enabled: true\nprocesses:\n  - name: x\n    command: go\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAPI.Host != "127.0.0.1" || cfg.HTTPAPI.Port != 8080 {
		t.Fatalf("defaults HTTP inesperados: %+v", cfg.HTTPAPI)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	// Verificar que se devuelva error si el archivo no existe (no hacer panic!)
	_, err := LoadConfig("no_existe_este_archivo.yaml")
	if err == nil {
		t.Error("se esperaba error al leer un archivo que no existe")
	}
}
