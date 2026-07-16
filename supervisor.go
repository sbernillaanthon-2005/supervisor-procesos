package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// ProcessState enumera los posibles estados de un proceso supervisado
type ProcessState string

const (
	StateStopped ProcessState = "stopped"
	StateRunning ProcessState = "running"
	StateBackoff ProcessState = "backoff"
	StateFailed  ProcessState = "failed"
)

// ProcessStatus agrupa el estado y metadatos de un proceso para observabilidad y tests
type ProcessStatus struct {
	State        ProcessState
	RestartCount int
}

// Supervisor se encarga de manejar el ciclo de vida de los procesos concurrentemente.
type Supervisor struct {
	Config  *Config
	LogsDir string
	wg      sync.WaitGroup

	// Estado protegido por mutex para evitar data-races
	mu       sync.Mutex
	statuses map[string]*ProcessStatus
}

// NewSupervisor crea una nueva instancia del supervisor.
func NewSupervisor(cfg *Config, logsDir string) *Supervisor {
	return &Supervisor{
		Config:   cfg,
		LogsDir:  logsDir,
		statuses: make(map[string]*ProcessStatus),
	}
}

// initStatus inicializa de manera segura el status de un proceso
func (s *Supervisor) initStatus(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.statuses[name]; !exists {
		s.statuses[name] = &ProcessStatus{State: StateStopped, RestartCount: 0}
	}
}

// SetState actualiza el estado (State Machine transition)
func (s *Supervisor) SetState(name string, state ProcessState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.statuses[name]; ok {
		stat.State = state
	}
}

// GetState devuelve el estado actual (útil para tests o el H5)
func (s *Supervisor) GetState(name string) ProcessState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.statuses[name]; ok {
		return stat.State
	}
	return StateStopped
}

// GetStartCount devuelve cuántas veces ha arrancado un proceso (útil para tests).
func (s *Supervisor) GetStartCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.statuses[name]; ok {
		return stat.RestartCount
	}
	return 0
}

// incrementStartCount incrementa la cantidad de arranques del proceso
func (s *Supervisor) incrementStartCount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stat, ok := s.statuses[name]; ok {
		stat.RestartCount++
	}
}

// Start lanza todos los procesos y los supervisa según su política de reinicio.
func (s *Supervisor) Start(ctx context.Context) {
	for _, proc := range s.Config.Processes {
		s.wg.Add(1)

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

// superviseProcess es el bucle de ciclo de vida e implementa la Máquina de Estados oficial
func (s *Supervisor) superviseProcess(ctx context.Context, proc ProcessConfig) {
	s.initStatus(proc.Name)
	failuresInARow := 0

	for {
		if ctx.Err() != nil {
			s.SetState(proc.Name, StateStopped)
			log.Printf("[INFO] [%s] Contexto cancelado antes de arrancar. Estado -> STOPPED.", proc.Name)
			return
		}

		// TRANSICIÓN: RUNNING
		s.SetState(proc.Name, StateRunning)
		log.Printf("[INFO] [%s] Arrancando proceso...", proc.Name)
		err := s.runProcess(ctx, proc)

		exitCode := 0
		if err != nil {
			log.Printf("[ERROR] [%s] Terminó con error: %v", proc.Name, err)
			if exitError, ok := err.(*exec.ExitError); ok {
				exitCode = exitError.ExitCode()
			} else {
				exitCode = -1
			}
		} else {
			log.Printf("[INFO] [%s] Terminó limpiamente (exit code 0)", proc.Name)
		}

		if ctx.Err() != nil {
			s.SetState(proc.Name, StateStopped)
			log.Printf("[INFO] [%s] Supervisor apagándose tras ejecución. Estado -> STOPPED.", proc.Name)
			return
		}

		if exitCode != 0 {
			failuresInARow++
		} else {
			// Si sale limpiamente, reseteamos el contador de fallos seguidos
			failuresInARow = 0
		}

		shouldRestart := false
		switch proc.RestartPolicy {
		case "always":
			shouldRestart = true
		case "on-failure":
			shouldRestart = (exitCode != 0)
		case "never":
			shouldRestart = false
		default:
			shouldRestart = false
		}

		if !shouldRestart {
			s.SetState(proc.Name, StateStopped)
			log.Printf("[INFO] [%s] Política '%s'. No se reinicia. Estado -> STOPPED.", proc.Name, proc.RestartPolicy)
			return
		}

		// LOGICA DE BACKOFF (Solo ante fallos continuos para evitar tormentas)
		if failuresInARow > 0 {
			bo := proc.Backoff
			if bo == nil {
				bo = &BackoffConfig{}
			}

			// Sanear Zero Values campo por campo
			base := bo.Base
			if base <= 0 {
				base = 1 * time.Second
			}
			factor := bo.Factor
			if factor <= 1.0 { // Factor 0 o 1 anulan la progresión exponencial
				factor = 2.0
			}
			maxWait := bo.Max
			if maxWait <= 0 {
				maxWait = 8 * time.Second
			}
			maxTries := bo.MaxTries
			if maxTries == 0 {
				maxTries = 5 // Default seguro (Opción A)
			}

			// Nota: maxTries < 0 (ej. -1) se interpretará como "infinito" (sin tope).
			if maxTries > 0 && failuresInARow > maxTries {
				// TRANSICIÓN: FAILED
				s.SetState(proc.Name, StateFailed)
				log.Printf("[INFO] [%s] Alcanzó el tope de %d reintentos fallidos. Estado -> FAILED.", proc.Name, maxTries)
				return
			}

			// Cálculo Exponencial: wait = base * (factor ^ (fails - 1))
			waitTime := float64(base) * math.Pow(factor, float64(failuresInARow-1))
			waitDur := time.Duration(waitTime)
			if waitDur > maxWait {
				waitDur = maxWait
			}

			// TRANSICIÓN: BACKOFF
			s.SetState(proc.Name, StateBackoff)
			log.Printf("[INFO] [%s] Backoff (Fallo #%d): esperando %v antes de reiniciar...", proc.Name, failuresInARow, waitDur)

			// Espera con aborto temprano si se cancela el contexto global
			select {
			case <-time.After(waitDur):
				// Continúa el loop, volverá a RUNNING
			case <-ctx.Done():
				s.SetState(proc.Name, StateStopped)
				return
			}
		} else {
			// Si salió bien (exit 0) pero la política es "always",
			// aplicamos un rate-limit pequeñísimo por seguridad (como lo hace systemd).
			// Usamos select para mantener un apagado limpio inmediato si ocurre aquí.
			select {
			case <-time.After(100 * time.Millisecond):
				// Sigue
			case <-ctx.Done():
				s.SetState(proc.Name, StateStopped)
				return
			}
		}
	}
}

// runProcess crea y ejecuta el comando.
func (s *Supervisor) runProcess(ctx context.Context, proc ProcessConfig) error {
	s.incrementStartCount(proc.Name)

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
		return fmt.Errorf("fallo al arrancar: %w", err)
	}

	return cmd.Wait()
}
