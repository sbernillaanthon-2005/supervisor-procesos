//go:build !windows

package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func terminateProcess(p *os.Process, requested string, name string) error {
	var sig syscall.Signal
	switch strings.ToUpper(requested) {
	case "", "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGHUP":
		sig = syscall.SIGHUP
	case "SIGKILL":
		sig = syscall.SIGKILL
	default:
		sig = syscall.SIGTERM
	}
	log.Printf("[INFO] [%s] enviando señal Unix %s", name, sig)
	return p.Signal(sig)
}

func ListenReloadSignal(s *Supervisor, path string) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				cfg, err := LoadConfig(path)
				if err != nil {
					log.Printf("[ERROR] recarga rechazada: %v", err)
					continue
				}
				if err := s.ReloadConfig(cfg); err != nil {
					log.Printf("[ERROR] recarga fallida: %v", err)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
