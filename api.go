package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type ProcessStatusInfo struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
	Starts   int    `json:"starts"`
}
type StatusResponse struct {
	Processes []ProcessStatusInfo `json:"processes"`
}
type ActionResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}
type ErrorResponse struct {
	Error string `json:"error"`
}

type HTTPServer struct {
	server     *http.Server
	sv         *Supervisor
	configPath string
	errCh      chan error
}

func NewHTTPServer(sv *Supervisor, port int) *HTTPServer {
	return NewHTTPServerForConfig(sv, "127.0.0.1", port, "")
}

func NewHTTPServerForConfig(sv *Supervisor, host string, port int, configPath string) *HTTPServer {
	if host == "" {
		host = "127.0.0.1"
	}
	mux := http.NewServeMux()
	hs := &HTTPServer{sv: sv, configPath: configPath, errCh: make(chan error, 1)}
	mux.HandleFunc("/status", hs.handleStatus)
	mux.HandleFunc("/processes/", hs.handleProcesses)
	mux.HandleFunc("/reload", hs.handleReload)
	hs.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return hs
}

func (hs *HTTPServer) Start() {
	log.Printf("[INFO] API HTTP en http://%s", hs.server.Addr)
	go func() {
		err := hs.server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		hs.errCh <- err
	}()
}
func (hs *HTTPServer) Shutdown(ctx context.Context) error { return hs.server.Shutdown(ctx) }
func (hs *HTTPServer) Wait() error                        { return <-hs.errCh }

func (hs *HTTPServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, http.StatusMethodNotAllowed, "método no soportado")
		return
	}
	sendJSON(w, http.StatusOK, StatusResponse{Processes: hs.sv.StatusSnapshot()})
}

func (hs *HTTPServer) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "método no soportado")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/processes/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		sendError(w, http.StatusBadRequest, "ruta inválida")
		return
	}
	var err error
	switch parts[1] {
	case "start":
		err = hs.sv.StartProcess(parts[0])
	case "stop":
		err = hs.sv.StopProcess(parts[0])
	case "restart":
		err = hs.sv.RestartProcess(parts[0])
	default:
		sendError(w, http.StatusBadRequest, "acción inválida")
		return
	}
	if err != nil {
		sendSupervisorError(w, err)
		return
	}
	sendJSON(w, http.StatusOK, ActionResponse{Message: "operación completada", Status: "completed"})
}

func (hs *HTTPServer) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "método no soportado")
		return
	}
	if hs.configPath == "" {
		sendError(w, http.StatusInternalServerError, "ruta de configuración no disponible")
		return
	}
	cfg, err := LoadConfig(hs.configPath)
	if err == nil {
		err = hs.sv.ReloadConfig(cfg)
	}
	if err != nil {
		sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	sendJSON(w, http.StatusOK, ActionResponse{Message: "configuración recargada", Status: "completed"})
}

func sendSupervisorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProcessNotFound):
		sendError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrConflict):
		sendError(w, http.StatusConflict, err.Error())
	default:
		sendError(w, http.StatusInternalServerError, err.Error())
	}
}
func sendJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func sendError(w http.ResponseWriter, status int, msg string) {
	sendJSON(w, status, ErrorResponse{Error: msg})
}
