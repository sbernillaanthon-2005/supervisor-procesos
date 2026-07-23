package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Uso: %s <config.yaml>", os.Args[0])
	}
	configPath := os.Args[1]
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sv := NewSupervisor(cfg, "logs")
	stopReload := ListenReloadSignal(sv, configPath)
	defer stopReload()
	sv.Start(ctx)

	var api *HTTPServer
	apiDone := make(chan error, 1)
	apiAlreadyWaited := false
	if cfg.HTTPAPI.Enabled {
		api = NewHTTPServerForConfig(sv, cfg.HTTPAPI.Host, cfg.HTTPAPI.Port, configPath)
		api.Start()
		go func() { apiDone <- api.Wait() }()
	}

	if api != nil {
		select {
		case <-ctx.Done():
		case err := <-apiDone:
			apiAlreadyWaited = true
			if err != nil {
				log.Printf("[ERROR] API HTTP: %v", err)
			}
			stop()
		}
	} else {
		<-ctx.Done()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if api != nil {
		if err := api.Shutdown(shutdownCtx); err != nil {
			log.Printf("[ERROR] cierre API: %v", err)
		}
		if !apiAlreadyWaited {
			<-apiDone
		}
	}
	sv.Wait()
	log.Printf("[INFO] supervisor detenido")
}
