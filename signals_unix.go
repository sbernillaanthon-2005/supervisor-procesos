//go:build !windows

package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// sendSignal maneja el envío de señales a un proceso.
func sendSignal(p *os.Process, sigStr string, name string) error {
	var sig os.Signal
	switch strings.ToUpper(sigStr) {
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGQUIT":
		sig = syscall.SIGQUIT
	case "SIGTERM", "":
		sig = syscall.SIGTERM
	default:
		log.Printf("[WARN] [%s] Señal desconocida '%s', usando SIGTERM", name, sigStr)
		sig = syscall.SIGTERM
	}

	log.Printf("[INFO] [%s] Enviando señal %v para apagado ordenado", name, sig)
	return p.Signal(sig)
}

// ListenReloadSignal en Unix atrapa SIGHUP para recargar la configuración.
func ListenReloadSignal(s *Supervisor, configPath string) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	go func() {
		for sig := range sigs {
			log.Printf("\n[INFO] Señal de sistema %v recibida. Recargando configuración...", sig)
			cfg, err := LoadConfig(configPath)
			if err != nil {
				log.Printf("[ERROR] Fallo al recargar configuración: %v", err)
				continue
			}
			s.ReloadConfig(cfg)
		}
	}()
}
