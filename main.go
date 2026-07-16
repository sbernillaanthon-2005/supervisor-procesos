package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Uso: %s <ruta_al_config.yaml>", os.Args[0])
	}

	configPath := os.Args[1]
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}

	// 1. Asegurar que el directorio de logs exista
	logsDir := "./logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		log.Fatalf("Error creando directorio de logs: %v", err)
	}

	// HITO 1: Lanzar cada proceso
	// Aunque el reinicio se hace en el H2, necesitamos lanzar todos los procesos concurrentemente
	// de lo contrario un proceso que no termina bloquearía al siguiente en el bucle 'for'.
	// Usamos sync.WaitGroup para evitar que el supervisor (main) finalice de inmediato.
	var wg sync.WaitGroup

	for _, proc := range cfg.Processes {
		wg.Add(1)

		// Lanzamos una goroutine (hilo ligero) por cada proceso
		// Pasamos proc como argumento de la función anónima por seguridad de concurrencia
		go func(p ProcessConfig) {
			defer wg.Done()

			fmt.Printf("[INFO] Arrancando proceso: %s...\n", p.Name)
			if err := startProcess(p, logsDir); err != nil {
				log.Printf("[ERROR] Proceso %s terminó con error o no pudo arrancar: %v", p.Name, err)
			} else {
				fmt.Printf("[INFO] Proceso %s terminó exitosamente.\n", p.Name)
			}
		}(proc)
	}

	fmt.Println("[INFO] Supervisor iniciado. Esperando a la finalización inicial de procesos...")
	wg.Wait() // Bloquea main hasta que todas las goroutines llamen wg.Done()
	fmt.Println("[INFO] Todos los procesos han terminado. Saliendo.")
}

// startProcess prepara y arranca un proceso individual bloqueando su goroutine hasta que termine.
func startProcess(proc ProcessConfig, logsDir string) error {
	cmd := exec.Command(proc.Command, proc.Args...)

	if proc.WorkingDir != "" {
		cmd.Dir = proc.WorkingDir
	}

	// Combinar el environment actual del sistema con el especificado en config
	cmd.Env = os.Environ()
	for k, v := range proc.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Preparar archivos de log (stdout y stderr)
	outLogPath := filepath.Join(logsDir, fmt.Sprintf("%s.out.log", proc.Name))
	errLogPath := filepath.Join(logsDir, fmt.Sprintf("%s.err.log", proc.Name))

	// Abrir/crear archivos para escritura.
	// Usamos O_APPEND para que cada vez que arranque agregue al final (útil en H2 para reinicios).
	outFile, err := os.OpenFile(outLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("no se pudo abrir out log: %w", err)
	}
	defer outFile.Close()

	errFile, err := os.OpenFile(errLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("no se pudo abrir err log: %w", err)
	}
	defer errFile.Close()

	// Conectar el stream del comando a nuestros archivos
	cmd.Stdout = outFile
	cmd.Stderr = errFile

	// cmd.Start arranca el proceso asíncronamente
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("fallo al arrancar (cmd.Start): %w", err)
	}

	// cmd.Wait bloquea la goroutine actual hasta que el proceso hijo finalice (éxito o error).
	// Esto es esencial para H1 y clave para que en H2 detectemos que finalizó.
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("fallo durante ejecución o salida no-cero: %w", err)
	}

	return nil
}
