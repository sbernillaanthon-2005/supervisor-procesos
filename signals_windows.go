//go:build windows

package main

import (
	"log"
	"os"
)

// sendSignal maneja el envío de señales a un proceso.
// En Windows, enviar señales como SIGTERM a un proceso de consola no está soportado nativamente
// por os.Process.Signal (retornará "not supported by windows").
func sendSignal(p *os.Process, sigStr string, name string) error {
	if sigStr == "" {
		sigStr = "SIGTERM"
	}
	log.Printf("[WARN] [%s] Windows no soporta señal '%s' de forma nativa. Forzando SIGKILL.", name, sigStr)
	return p.Kill()
}

// ListenReloadSignal en Windows no hace nada, porque SIGHUP no existe.
// Para recargar la configuración en Windows, se debe llamar a ReloadConfig programáticamente
// o implementar un endpoint alternativo.
func ListenReloadSignal(s *Supervisor, configPath string) {
	log.Printf("[INFO] SIGHUP no está soportado en Windows. Usa recarga programática.")
}
