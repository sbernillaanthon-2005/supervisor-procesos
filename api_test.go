package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAPI_Status(t *testing.T) {
	// Preparar supervisor dummy
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "p1"},
			{Name: "p2"},
		},
	}
	sv := NewSupervisor(cfg, t.TempDir())
	sv.initStatus("p1")
	sv.SetState("p1", StateRunning)
	sv.incrementStartCount("p1")
	sv.initStatus("p2")
	sv.SetState("p2", StateFailed)
	sv.incrementStartCount("p2")
	sv.incrementStartCount("p2")

	hs := NewHTTPServer(sv, 0)

	// Crear petición GET /status
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()

	hs.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Esperaba código 200, obtuvo %d", rr.Code)
	}

	var resp StatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Fallo al parsear JSON: %v", err)
	}

	if len(resp.Processes) != 2 {
		t.Fatalf("Esperaba 2 procesos, obtuvo %d", len(resp.Processes))
	}

	// Validar que los estados se mapean correctamente
	for _, p := range resp.Processes {
		if p.Name == "p1" {
			if p.State != "running" || p.Starts != 1 {
				t.Errorf("Estado incorrecto para p1: %+v", p)
			}
		} else if p.Name == "p2" {
			if p.State != "failed" || p.Starts != 2 {
				t.Errorf("Estado incorrecto para p2: %+v", p)
			}
		}
	}
}

func TestAPI_Reload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("processes:\n  - name: p1\n    command: go\n    args: [version]\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	sv := NewSupervisor(cfg, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sv.Start(ctx)
	hs := NewHTTPServerForConfig(sv, "127.0.0.1", 0, path)

	req := httptest.NewRequest(http.MethodPost, "/reload", nil)
	rr := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /reload = %d: %s", rr.Code, rr.Body.String())
	}
	sv.Wait()
}

func TestAPI_Conflict(t *testing.T) {
	cfg := &Config{Processes: []ProcessConfig{{Name: "p1", Command: "go", Args: []string{"version"}}}}
	sv := NewSupervisor(cfg, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sv.mu.Lock()
	sv.globalCtx = ctx
	sv.mu.Unlock()
	if err := sv.StartProcess("p1"); err != nil {
		t.Fatal(err)
	}
	hs := NewHTTPServer(sv, 0)
	req := httptest.NewRequest(http.MethodPost, "/processes/p1/start", nil)
	rr := httptest.NewRecorder()
	hs.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("inicio duplicado = %d; se esperaba 409", rr.Code)
	}
	sv.Wait()
}

func TestAPI_MethodNotAllowed(t *testing.T) {
	sv := NewSupervisor(&Config{}, t.TempDir())
	hs := NewHTTPServer(sv, 0)

	req := httptest.NewRequest(http.MethodPost, "/status", nil)
	rr := httptest.NewRecorder()

	hs.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Esperaba código 405 para POST /status, obtuvo %d", rr.Code)
	}
}

func TestAPI_ProcessesActions(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "p1", Command: "go", Args: []string{"version"}},
		},
	}
	sv := NewSupervisor(cfg, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		sv.Wait() // Clean up goroutines to avoid leaks
	}()
	sv.Start(ctx) // Officially start supervisor

	hs := NewHTTPServer(sv, 0)

	tests := []struct {
		method       string
		path         string
		expectedCode int
		expectedMsg  string // Substring a buscar en JSON de éxito o error
	}{
		{http.MethodGet, "/processes/p1/stop", http.StatusMethodNotAllowed, "método no soportado"},
		{http.MethodPost, "/processes/p2/stop", http.StatusNotFound, "no encontrado"},
		{http.MethodPost, "/processes/p1/stop", http.StatusOK, "señal de detención enviada"},
		{http.MethodPost, "/processes/p1/start", http.StatusOK, "señal de arranque enviada"},
		{http.MethodPost, "/processes/p1/restart", http.StatusOK, "señal de reinicio enviada"},
		{http.MethodPost, "/processes/p1/invalid", http.StatusBadRequest, "acción inválida"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rr := httptest.NewRecorder()

		hs.server.Handler.ServeHTTP(rr, req)

		if rr.Code != tc.expectedCode {
			t.Errorf("Para %s %s: esperaba código %d, obtuvo %d", tc.method, tc.path, tc.expectedCode, rr.Code)
		}

		if tc.expectedCode == http.StatusOK {
			var resp ActionResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err == nil {
				if resp.Status != "completed" {
					t.Errorf("Para %s: esperaba status 'completed', obtuvo '%s'", tc.path, resp.Status)
				}
			}
		}
	}
}

func TestAPI_UnstartedSupervisor(t *testing.T) {
	cfg := &Config{
		Processes: []ProcessConfig{
			{Name: "p1", Command: "go", Args: []string{"version"}},
		},
	}
	sv := NewSupervisor(cfg, t.TempDir())
	// NOTE: We deliberately do NOT call sv.Start() here
	hs := NewHTTPServer(sv, 0)

	req := httptest.NewRequest(http.MethodPost, "/processes/p1/start", nil)
	rr := httptest.NewRecorder()

	hs.server.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Esperaba código 500 para supervisor no iniciado, obtuvo %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err == nil {
		if resp.Error != "supervisor no ha sido iniciado aún" {
			t.Errorf("Mensaje de error inesperado: %s", resp.Error)
		}
	}
}
