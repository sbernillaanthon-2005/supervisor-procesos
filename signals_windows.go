//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
)

// terminateProcess finaliza el árbol administrado. Windows no ofrece una
// señal equivalente a SIGTERM para aplicaciones de consola arbitrarias.
func terminateProcess(p *os.Process, _ string, name string) error {
	if p == nil {
		return nil
	}
	log.Printf("[WARN] [%s] Windows: finalizando el árbol de procesos; no es un SIGTERM ordenado", name)
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(p.Pid), "/T", "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		if killErr := p.Kill(); killErr != nil && !errorsProcessDone(killErr) {
			return fmt.Errorf("taskkill: %v (%s); Kill: %w", err, out, killErr)
		}
	}
	return nil
}

func errorsProcessDone(err error) bool { return err == os.ErrProcessDone }

func ListenReloadSignal(_ *Supervisor, _ string) func() {
	log.Printf("[INFO] Windows: recarga disponible mediante POST /reload")
	return func() {}
}
