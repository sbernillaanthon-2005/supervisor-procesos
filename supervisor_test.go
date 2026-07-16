package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisor_BackoffAndState(t *testing.T) {
	tmpLogs := t.TempDir()

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_fast_fail",
				Command:       "go",
				Args:          []string{"run", "non_existent.go"}, // Falla rápidamente
				RestartPolicy: "on-failure",
				// Inyectamos backoff súper rápido para el test
				Backoff: &BackoffConfig{
					Base:     10 * time.Millisecond,
					Factor:   2.0,
					Max:      50 * time.Millisecond,
					MaxTries: 3, // Fallará la primera vez, luego intentará 3 veces más y se rendirá.
				},
			},
		},
	}

	sv := NewSupervisor(cfg, tmpLogs)

	// Le damos suficiente tiempo para que agote los 3 reintentos (que son fallos).
	// Tiempo de espera total de backoffs: 10ms + 20ms + 40ms = 70ms.
	// Así que un timeout de 500ms es muy holgado para no tener tests frágiles,
	// pero mucho mejor que los típicos tests que duermen 5 segundos.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	sv.Start(ctx)
	sv.Wait() // Bloquea hasta que la máquina de estados termine en FAILED (por maxTries) o se cancele el ctx.

	// 1. Verificar Estado Final: debe haber alcanzado el límite de intentos y quedado como FAILED
	if st := sv.GetState("proc_fast_fail"); st != StateFailed {
		t.Errorf("Esperaba estado 'failed', obtuvo '%s'", st)
	}

	// 2. Verificar Contador: 1 arranque original + 3 reintentos = 4 arranques totales
	count := sv.GetStartCount("proc_fast_fail")
	if count != 4 {
		t.Errorf("Esperaba 4 arranques totales, obtuvo %d", count)
	}
}

func TestSupervisor_RunAndCancel(t *testing.T) {
	// Mantenemos el test de cancelación básico del H2 para asegurar
	// que políticas básicas sigan andando sin data races.
	tmpLogs := t.TempDir()

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_always",
				Command:       "go",
				Args:          []string{"version"},
				RestartPolicy: "always",
			},
			{
				Name:          "proc_never",
				Command:       "go",
				Args:          []string{"version"},
				RestartPolicy: "never",
			},
		},
	}

	sv := NewSupervisor(cfg, tmpLogs)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	sv.Start(ctx)
	sv.Wait()

	countAlways := sv.GetStartCount("proc_always")
	if countAlways <= 1 {
		t.Errorf("proc_always debía reiniciar, pero arrancó %d veces", countAlways)
	}

	countNever := sv.GetStartCount("proc_never")
	if countNever != 1 {
		t.Errorf("proc_never debía arrancar 1 vez, pero arrancó %d veces", countNever)
	}

	// Validar que terminen marcados como STOPPED al recibir cancelación / nunca reiniciar
	if st := sv.GetState("proc_always"); st != StateStopped {
		t.Errorf("proc_always debía terminar 'stopped', obtuvo '%s'", st)
	}
	if st := sv.GetState("proc_never"); st != StateStopped {
		t.Errorf("proc_never debía terminar 'stopped', obtuvo '%s'", st)
	}

	for _, p := range cfg.Processes {
		outPath := filepath.Join(tmpLogs, p.Name+".out.log")
		if _, err := os.Stat(outPath); os.IsNotExist(err) {
			t.Errorf("No se encontró log de salida para %s.", p.Name)
		}
	}
}

func TestSupervisor_BackoffZeroValues(t *testing.T) {
	tmpLogs := t.TempDir()

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_zero_backoff",
				Command:       "go",
				Args:          []string{"run", "non_existent.go"}, // Falla rápidamente
				RestartPolicy: "on-failure",
				// Inyectamos backoff con valores intencionalmente en cero
				Backoff: &BackoffConfig{
					Base:     0,
					Factor:   0,
					Max:      0,
					MaxTries: 2, // Limite bajo para test rápido
				},
			},
		},
	}

	sv := NewSupervisor(cfg, tmpLogs)

	// Con defaults seguros, Base será 1s y Factor 2.0. MaxTries=2 tomará:
	// Intento 1 (falla) -> Espera 1s -> Intento 2 (falla) -> Espera 2s -> Intento 3 (falla) -> Límite alcanzado (FAILED).
	// Como queremos que este test no tarde 3 segundos, lo cancelamos nosotros antes si todo va bien,
	// o simplemente dejamos que agote el tiempo para comprobar que NO hace una "tormenta de reinicios".
	// Si el bug existiera, arrancaría cientos de veces en estos 50ms.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	sv.Start(ctx)
	sv.Wait() // Espera a que termine por cancelación

	count := sv.GetStartCount("proc_zero_backoff")
	// Si aplicó los defaults (1s espera), en 50ms solo pudo haber arrancado 1 vez y quedarse esperando en backoff.
	if count > 1 {
		t.Errorf("Esperaba máximo 1 arranque (protegido por defaults), obtuvo %d", count)
	}
}

func TestSupervisor_BackoffInfinite(t *testing.T) {
	tmpLogs := t.TempDir()

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_infinite_backoff",
				Command:       "go",
				Args:          []string{"run", "non_existent.go"}, // Falla rápidamente
				RestartPolicy: "on-failure",
				Backoff: &BackoffConfig{
					Base:     1 * time.Millisecond,
					Factor:   1.0, // Forzamos que lo limpie a 2.0
					Max:      2 * time.Millisecond,
					MaxTries: -1, // Infinito explícito
				},
			},
		},
	}

	sv := NewSupervisor(cfg, tmpLogs)

	// Le damos suficiente tiempo para arrancar unas 10 veces.
	// Ejecutar 'go run' toma ~30ms en Windows, así que 10 veces tomarán ~300ms.
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	sv.Start(ctx)
	sv.Wait() // Espera a que termine por cancelación

	count := sv.GetStartCount("proc_infinite_backoff")
	// Si el default de 5 aplicara por error, count sería 6 (1 base + 5 intentos).
	// Como pusimos -1, debe superar los 6.
	if count <= 6 {
		t.Errorf("Esperaba que intentara infinitamente (>6), pero se detuvo en %d arranques", count)
	}

	if st := sv.GetState("proc_infinite_backoff"); st != StateStopped {
		t.Errorf("Al ser infinito, debió terminar 'stopped' por la cancelación del contexto, pero terminó en '%s'", st)
	}
}

// TestSupervisor_ReloadConfig valida la lógica "diff" de recarga (H4) sin señales reales del SO.
func TestSupervisor_ReloadConfig(t *testing.T) {
	tmpLogs := t.TempDir()

	cfg1 := &Config{
		Processes: []ProcessConfig{
			{Name: "p1", Command: "go", Args: []string{"version"}, RestartPolicy: "always"},
			{Name: "p2", Command: "go", Args: []string{"version"}, RestartPolicy: "always"},
		},
	}

	sv := NewSupervisor(cfg1, tmpLogs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sv.Start(ctx)

	// Esperamos a que se registren
	time.Sleep(100 * time.Millisecond)

	sv.mu.Lock()
	if len(sv.cancelFuncs) != 2 {
		t.Fatalf("Esperaba 2 procesos iniciales en cancelFuncs, obtuvo %d", len(sv.cancelFuncs))
	}
	sv.mu.Unlock()

	// Configuración 2: p1 eliminado, p2 modificado (Args), p3 nuevo
	cfg2 := &Config{
		Processes: []ProcessConfig{
			{Name: "p2", Command: "go", Args: []string{"env"}, RestartPolicy: "always"},
			{Name: "p3", Command: "go", Args: []string{"version"}, RestartPolicy: "always"},
		},
	}

	// Ejecutar la recarga de forma concurrente para estresar el detector de races
	done := make(chan struct{})
	go func() {
		sv.ReloadConfig(cfg2)
		close(done)
	}()
	<-done

	time.Sleep(100 * time.Millisecond) // Dar tiempo a goroutines para arrancar/apagar

	sv.mu.Lock()
	if len(sv.cancelFuncs) != 2 {
		t.Fatalf("Esperaba 2 procesos tras recarga (p2, p3), obtuvo %d", len(sv.cancelFuncs))
	}
	if _, ok := sv.cancelFuncs["p1"]; ok {
		t.Errorf("p1 debía ser removido")
	}
	if _, ok := sv.cancelFuncs["p2"]; !ok {
		t.Errorf("p2 debía ser reiniciado y estar presente")
	}
	if _, ok := sv.cancelFuncs["p3"]; !ok {
		t.Errorf("p3 debía ser agregado")
	}
	sv.mu.Unlock()
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestSupervisor_GracefulShutdown valida programáticamente que la cancelación del child ctx
// detiene el proceso y lo marca como STOPPED, sin mandar señales del SO reales que puedan fallar en CI.
func TestSupervisor_GracefulShutdown(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Compilar un binario de prueba para que corra inmediatamente y se quede colgado
	sleepScript := filepath.Join(tmpDir, "sleep.go")
	os.WriteFile(sleepScript, []byte(`package main; import "time"; func main() { time.Sleep(10 * time.Second) }`), 0644)
	
	binFile := filepath.Join(tmpDir, "sleep_bin")
	if runtime.GOOS == "windows" {
		binFile += ".exe"
	}
	err := exec.Command("go", "build", "-o", binFile, sleepScript).Run()
	if err != nil {
		t.Fatalf("Falló al compilar script de prueba: %v", err)
	}

	// 2. Capturar log global para verificar la invocación de sendSignal
	var buf syncBuffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_graceful",
				Command:       binFile, // Proceso que duerme 10s
				RestartPolicy: "always",
				StopWait:      200 * time.Millisecond,
			},
		},
	}

	sv := NewSupervisor(cfg, tmpDir)
	
	// Iniciamos con el ctx global
	ctx, cancelGlobal := context.WithCancel(context.Background())
	defer cancelGlobal()
	sv.Start(ctx)

	// Darle tiempo para arrancar el binario (500ms para asegurar que pasó el exec)
	time.Sleep(500 * time.Millisecond)

	// Cancelamos puntualmente este proceso usando su func
	sv.mu.Lock()
	cancelProc, ok := sv.cancelFuncs["proc_graceful"]
	sv.mu.Unlock()

	if !ok {
		t.Fatalf("No se encontró cancelFunc para proc_graceful")
	}

	cancelProc() // Esto invoca la lógica de cmd.Cancel y WaitDelay programáticamente

	// Esperar que termine su graceful shutdown
	time.Sleep(300 * time.Millisecond)

	st := sv.GetState("proc_graceful")
	if st != StateStopped && st != StateFailed {
		t.Errorf("Esperaba que el proceso pasara a estado terminal tras cancelarlo, estado: %s", st)
	}

	// 3. Verificar que se invocó sendSignal
	logOut := buf.String()
	if !strings.Contains(logOut, "Forzando SIGKILL") && !strings.Contains(logOut, "Enviando señal") {
		t.Errorf("No se encontró evidencia de que sendSignal fue invocado. Logs: %s", logOut)
	}
}
