package main

import (
	"context"
	"testing"
	"time"
)

func TestSupervisor_RunAndCancel(t *testing.T) {
	// Setup: Directorio temporal para los logs de la prueba
	tmpLogs := t.TempDir()

	cfg := &Config{
		Processes: []ProcessConfig{
			{
				Name:          "proc_always",
				Command:       "go", // Un comando que finaliza rápido
				Args:          []string{"version"},
				RestartPolicy: "always",
			},
			{
				Name:          "proc_never",
				Command:       "go",
				Args:          []string{"version"},
				RestartPolicy: "never",
			},
			{
				Name:          "proc_fail",
				Command:       "go",
				Args:          []string{"run", "archivo_inexistente_123.go"}, // Forzamos un exit_code != 0
				RestartPolicy: "on-failure",
			},
		},
	}

	sv := NewSupervisor(cfg, tmpLogs)

	// Act: Creamos un contexto con timeout automático de 1 segundo
	// Esto verificará que el supervisor interrumpa los procesos limpiamente y
	// no haya data races en el manejo de context y goroutines compartidas.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	sv.Start(ctx)

	// Esperamos a que el timeout haga efecto y cierre todo
	sv.Wait()

	// Assert: Comprobar que todos corrieron la cantidad esperada de veces
	countAlways := sv.GetStartCount("proc_always")
	if countAlways <= 1 {
		t.Errorf("proc_always debía reiniciar múltiples veces, pero arrancó %d veces", countAlways)
	}

	countNever := sv.GetStartCount("proc_never")
	if countNever != 1 {
		t.Errorf("proc_never debía arrancar exactamente 1 vez, pero arrancó %d veces", countNever)
	}

	countFail := sv.GetStartCount("proc_fail")
	if countFail <= 1 {
		t.Errorf("proc_fail (on-failure) debía reiniciar múltiples veces, pero arrancó %d veces", countFail)
	}
}
