package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// ProcessStatusInfo represents the JSON response format for a single process.
type ProcessStatusInfo struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
}

// StatusResponse represents the JSON response format for the /status endpoint.
type StatusResponse struct {
	Processes []ProcessStatusInfo `json:"processes"`
}

// ActionResponse represents the JSON response format for control endpoints.
type ActionResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ErrorResponse represents the JSON response for errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HTTPServer encapuslates the HTTP API and the Supervisor reference.
type HTTPServer struct {
	server *http.Server
	sv     *Supervisor
}

// NewHTTPServer creates a new HTTPServer.
func NewHTTPServer(sv *Supervisor, port int) *HTTPServer {
	mux := http.NewServeMux()
	hs := &HTTPServer{
		sv: sv,
	}

	mux.HandleFunc("/status", hs.handleStatus)
	mux.HandleFunc("/processes/", hs.handleProcesses)

	hs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return hs
}

// Start launches the HTTP server in a goroutine.
func (hs *HTTPServer) Start() {
	log.Printf("[INFO] Iniciando servidor HTTP API en %s", hs.server.Addr)
	go func() {
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[ERROR] Servidor HTTP falló: %v", err)
		}
	}()
}

// Shutdown cleanly stops the HTTP server.
func (hs *HTTPServer) Shutdown(ctx context.Context) error {
	log.Printf("[INFO] Deteniendo servidor HTTP API...")
	return hs.server.Shutdown(ctx)
}

func (hs *HTTPServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "método no soportado")
		return
	}

	// Fetch statuses from supervisor
	hs.sv.mu.Lock()
	var processes []ProcessStatusInfo
	if hs.sv.Config != nil {
		for _, p := range hs.sv.Config.Processes {
			st := hs.sv.statuses[p.Name]
			if st != nil {
				processes = append(processes, ProcessStatusInfo{
					Name:     p.Name,
					State:    string(st.State),
					Restarts: st.RestartCount,
				})
			} else {
				processes = append(processes, ProcessStatusInfo{
					Name:     p.Name,
					State:    string(StateStopped),
					Restarts: 0,
				})
			}
		}
	}
	hs.sv.mu.Unlock()

	// Handle empty case to return `[]` instead of `null`
	if processes == nil {
		processes = make([]ProcessStatusInfo, 0)
	}

	resp := StatusResponse{Processes: processes}
	sendJSON(w, http.StatusOK, resp)
}

func (hs *HTTPServer) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "método no soportado")
		return
	}

	// URL format: /processes/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/processes/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		sendError(w, http.StatusBadRequest, "ruta inválida")
		return
	}

	name := parts[0]
	action := parts[1]

	var err error
	var msg string

	switch action {
	case "stop":
		err = hs.sv.StopProcess(name)
		msg = "señal de detención enviada"
	case "start":
		err = hs.sv.StartProcess(name)
		msg = "señal de arranque enviada"
	case "restart":
		err = hs.sv.RestartProcess(name)
		msg = "señal de reinicio enviada"
	default:
		sendError(w, http.StatusBadRequest, "acción inválida")
		return
	}

	if err != nil {
		if errors.Is(err, ErrProcessNotFound) {
			sendError(w, http.StatusNotFound, err.Error())
		} else {
			sendError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	sendJSON(w, http.StatusOK, ActionResponse{
		Message: msg,
		Status:  "pending",
	})
}

func sendJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func sendError(w http.ResponseWriter, status int, msg string) {
	sendJSON(w, status, ErrorResponse{Error: msg})
}
