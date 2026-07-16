package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
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

	logsDir := "./logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		log.Fatalf("Error creando directorio de logs: %v", err)
	}

	// HITO 2: Coordinación con context.Context
	// Creamos un contexto base que podemos cancelar.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Buena práctica: asegurar liberación de recursos

	// Interceptamos Ctrl+C (SIGINT) o señales de terminación del SO (SIGTERM)
	// para probar que podemos cancelar todo limpiamente.
	// NOTA: El apagado ordenado completo (grace period antes del kill) va en H4.
	// Por ahora en H2, cancel() activa el SIGKILL que exec.CommandContext tiene por defecto.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("\n[INFO] Señal de sistema %v recibida. Cancelando el contexto global (H2/H4)...", sig)
		cancel() // Esto viaja a todas las goroutines e interrumpe los exec.CommandContext, activando el apagado ordenado de H4
	}()

	sv := NewSupervisor(cfg, logsDir)

	// H4: Escuchar señal de recarga (SIGHUP en Unix, dummy en Windows)
	ListenReloadSignal(sv, configPath)

	log.Println("[INFO] Iniciando supervisor. Presione Ctrl+C para detener.")
	sv.Start(ctx)

	// H5: Iniciar el servidor HTTP si está habilitado
	var apiServer *HTTPServer
	if cfg.HTTPAPI.Enabled {
		port := cfg.HTTPAPI.Port
		if port == 0 {
			port = 8080 // default
		}
		apiServer = NewHTTPServer(sv, port)
		apiServer.Start()

		// Goroutine para apagar el servidor HTTP limpiamente al cancelar el contexto
		go func() {
			<-ctx.Done()
			apiServer.Shutdown(context.Background())
		}()
	}

	// Esperamos a que todas las goroutines supervisadas terminen.
	// Si un proceso es "always", esta línea bloqueará indefinidamente hasta presionar Ctrl+C.
	sv.Wait()
	log.Println("[INFO] Todos los procesos han terminado. Saliendo del programa principal.")
}
