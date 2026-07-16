package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Supervisor se encarga de manejar el ciclo de vida de los procesos concurrentemente.
type Supervisor struct {
	Config  *Config
	LogsDir string
	wg      sync.WaitGroup

	// Contador thread-safe para pruebas y observabilidad
	mu          sync.Mutex
	startCounts map[string]int
}

// NewSupervisor crea una nueva instancia del supervisor.
func NewSupervisor(cfg *Config, logsDir string) *Supervisor {
	return &Supervisor{
		Config:      cfg,
		LogsDir:     logsDir,
		startCounts: make(map[string]int),
	}
}

// Start lanza todos los procesos y los supervisa según su política de reinicio.
// Recibe un context para propagar la cancelación.
func (s *Supervisor) Start(ctx context.Context) {
	for _, proc := range s.Config.Processes {
		s.wg.Add(1)

		// Cada proceso corre en su propia goroutine
		go func(p ProcessConfig) {
			defer s.wg.Done()
			s.superviseProcess(ctx, p)
		}(proc)
	}
}

// Wait bloquea hasta que todas las goroutines supervisadas hayan terminado.
func (s *Supervisor) Wait() {
	s.wg.Wait()
}

// superviseProcess es el bucle de ciclo de vida de un solo proceso.
func (s *Supervisor) superviseProcess(ctx context.Context, proc ProcessConfig) {
	for {
		// 1. Antes de arrancar, comprobamos si ya se pidió cancelar el supervisor
		if ctx.Err() != nil {
			log.Printf("[INFO] [%s] Contexto cancelado. Deteniendo supervisión.", proc.Name)
			return
		}

		log.Printf("[INFO] [%s] Arrancando proceso...", proc.Name)
		err := s.runProcess(ctx, proc)

		// 2. Evaluamos la salida
		exitCode := 0
		if err != nil {
			log.Printf("[ERROR] [%s] Terminó con error: %v", proc.Name, err)
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				// Fallo al iniciar u otro error de I/O
				exitCode = -1
			}
		} else {
			log.Printf("[INFO] [%s] Terminó limpiamente (exit code 0)", proc.Name)
		}

		// 3. Verificamos nuevamente si se canceló DURANTE la ejecución
		if ctx.Err() != nil {
			log.Printf("[INFO] [%s] Supervisor apagándose. Ignorando política de reinicio.", proc.Name)
			return
		}

		// 4. Decidimos si reiniciamos según política
		shouldRestart := false
		switch proc.RestartPolicy {
		case "always":
			shouldRestart = true
		case "on-failure":
			shouldRestart = (exitCode != 0)
		case "never":
			shouldRestart = false
		default: // Por defecto 'never' si hay un error de tipeo en el YAML
			shouldRestart = false
		}

		if !shouldRestart {
			log.Printf("[INFO] [%s] Política '%s'. No se reinicia más. Saliendo.", proc.Name, proc.RestartPolicy)
			return
		}

		log.Printf("[INFO] [%s] Política '%s' aplica. Reiniciando...", proc.Name, proc.RestartPolicy)

		// NOTA: Para evitar que un proceso que falla instantáneamente nos congele la CPU
		// con un bucle infinito ("tormenta de reinicios"), ponemos un sleep mínimo aquí.
		// El "backoff exponencial" real y la "máquina de estados" se implementarán en el H3.
		time.Sleep(100 * time.Millisecond)
	}
}

// GetStartCount devuelve cuántas veces ha arrancado un proceso (útil para tests).
func (s *Supervisor) GetStartCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCounts[name]
}

// runProcess crea y ejecuta el comando, atándolo al contexto de cancelación.
func (s *Supervisor) runProcess(ctx context.Context, proc ProcessConfig) error {
	s.mu.Lock()
	s.startCounts[proc.Name]++
	s.mu.Unlock()

	// IMPORTANTE H2: exec.CommandContext vincula el proceso al context de Go.
	// Si ctx se cancela, Go automáticamente le envía un SIGKILL al hijo.
	cmd := exec.CommandContext(ctx, proc.Command, proc.Args...)

	if proc.WorkingDir != "" {
		cmd.Dir = proc.WorkingDir
	}

	cmd.Env = os.Environ()
	for k, v := range proc.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	outLogPath := filepath.Join(s.LogsDir, fmt.Sprintf("%s.out.log", proc.Name))
	errLogPath := filepath.Join(s.LogsDir, fmt.Sprintf("%s.err.log", proc.Name))

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

	cmd.Stdout = outFile
	cmd.Stderr = errFile

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("fallo al iniciar proceso: %w", err)
	}

	// cmd.Wait bloquea hasta que finalice el proceso o el ctx se cancele
	return cmd.Wait()
}
