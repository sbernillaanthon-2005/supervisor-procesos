package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

var (
	ErrProcessNotFound = errors.New("proceso no encontrado")
	ErrConflict        = errors.New("transición incompatible")
)

type ProcessState string

const (
	StateStopped  ProcessState = "stopped"
	StateRunning  ProcessState = "running"
	StateBackoff  ProcessState = "backoff"
	StateFailed   ProcessState = "failed"
	StateStopping ProcessState = "stopping"
)

// RestartCount cuenta reinicios automáticos; StartCount cuenta todos los arranques.
type ProcessStatus struct {
	State        ProcessState
	RestartCount int
	StartCount   int
}

type managedProcess struct {
	config ProcessConfig
	status ProcessStatus
	cancel context.CancelFunc
	done   chan struct{}
}

type Supervisor struct {
	Config  *Config // conservado por compatibilidad; acceso interno siempre bajo mu
	LogsDir string

	mu          sync.Mutex
	processes   map[string]*managedProcess
	statuses    map[string]*ProcessStatus // vista compatible para pruebas antiguas
	cancelFuncs map[string]context.CancelFunc
	globalCtx   context.Context
	wg          sync.WaitGroup
}

func NewSupervisor(cfg *Config, logsDir string) *Supervisor {
	s := &Supervisor{
		Config:      cfg,
		LogsDir:     logsDir,
		processes:   make(map[string]*managedProcess),
		statuses:    make(map[string]*ProcessStatus),
		cancelFuncs: make(map[string]context.CancelFunc),
	}
	if cfg != nil {
		for _, p := range cfg.Processes {
			s.addProcessLocked(p)
		}
	}
	return s
}

func (s *Supervisor) addProcessLocked(p ProcessConfig) *managedProcess {
	m := &managedProcess{config: p, status: ProcessStatus{State: StateStopped}}
	s.processes[p.Name] = m
	s.statuses[p.Name] = &m.status
	return m
}

func (s *Supervisor) initStatus(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.processes[name]; !ok {
		s.addProcessLocked(ProcessConfig{Name: name})
	}
}

func (s *Supervisor) SetState(name string, state ProcessState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.processes[name]; m != nil {
		m.status.State = state
	}
}

func (s *Supervisor) GetState(name string) ProcessState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.processes[name]; m != nil {
		return m.status.State
	}
	return StateStopped
}

func (s *Supervisor) GetStartCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.processes[name]; m != nil {
		return m.status.StartCount
	}
	return 0
}

func (s *Supervisor) incrementStartCount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.processes[name]; m != nil {
		m.status.StartCount++
		m.status.RestartCount = max(0, m.status.StartCount-1)
	}
}

func (s *Supervisor) ResetStartCount(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.processes[name]; m != nil {
		m.status.StartCount = 0
		m.status.RestartCount = 0
	}
}

func (s *Supervisor) Start(ctx context.Context) {
	s.mu.Lock()
	if s.globalCtx != nil {
		s.mu.Unlock()
		return
	}
	s.globalCtx = ctx
	names := make([]string, 0, len(s.processes))
	for name := range s.processes {
		names = append(names, name)
	}
	s.mu.Unlock()
	for _, name := range names {
		_ = s.StartProcess(name)
	}
}

func (s *Supervisor) StartProcess(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.globalCtx == nil {
		return errors.New("supervisor no ha sido iniciado aún")
	}
	m := s.processes[name]
	if m == nil {
		return fmt.Errorf("%w: %s", ErrProcessNotFound, name)
	}
	if m.done != nil {
		return fmt.Errorf("%w: %s ya está activo", ErrConflict, name)
	}
	if s.globalCtx.Err() != nil {
		return fmt.Errorf("%w: supervisor detenido", ErrConflict)
	}
	s.launchLocked(name, m)
	return nil
}

func (s *Supervisor) launchLocked(name string, m *managedProcess) {
	ctx, cancel := context.WithCancel(s.globalCtx)
	done := make(chan struct{})
	m.cancel, m.done = cancel, done
	s.cancelFuncs[name] = cancel
	proc := m.config
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.superviseProcess(ctx, proc)
		s.mu.Lock()
		if current := s.processes[name]; current == m && current.done == done {
			current.cancel, current.done = nil, nil
			delete(s.cancelFuncs, name)
		}
		close(done)
		s.mu.Unlock()
	}()
}

func (s *Supervisor) StopProcess(name string) error {
	s.mu.Lock()
	m := s.processes[name]
	if m == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrProcessNotFound, name)
	}
	if m.done == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s ya está detenido", ErrConflict, name)
	}
	m.status.State = StateStopping
	cancel, done := m.cancel, m.done
	s.mu.Unlock()
	cancel()
	<-done
	return nil
}

func (s *Supervisor) RestartProcess(name string) error {
	s.mu.Lock()
	if s.processes[name] == nil {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrProcessNotFound, name)
	}
	active := s.processes[name].done != nil
	s.mu.Unlock()
	if active {
		if err := s.StopProcess(name); err != nil {
			return err
		}
	}
	return s.StartProcess(name)
}

func (s *Supervisor) StatusSnapshot() []ProcessStatusInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ProcessStatusInfo, 0, len(s.processes))
	if s.Config == nil {
		return result
	}
	for _, p := range s.Config.Processes {
		m := s.processes[p.Name]
		info := ProcessStatusInfo{Name: p.Name, State: string(StateStopped)}
		if m != nil {
			info.State = string(m.status.State)
			info.Restarts = m.status.RestartCount
			info.Starts = m.status.StartCount
		}
		result = append(result, info)
	}
	return result
}

func (s *Supervisor) ReloadConfig(newConfig *Config) error {
	if err := newConfig.Validate(); err != nil {
		return err
	}
	newConfig.applyDefaults()
	s.mu.Lock()
	old := make(map[string]ProcessConfig, len(s.processes))
	for name, m := range s.processes {
		old[name] = m.config
	}
	s.mu.Unlock()

	next := make(map[string]ProcessConfig, len(newConfig.Processes))
	for _, p := range newConfig.Processes {
		next[p.Name] = p
	}
	for name, p := range old {
		np, exists := next[name]
		if !exists || !reflect.DeepEqual(p, np) {
			s.mu.Lock()
			active := s.processes[name] != nil && s.processes[name].done != nil
			s.mu.Unlock()
			if active {
				if err := s.StopProcess(name); err != nil && !errors.Is(err, ErrConflict) {
					return err
				}
			}
		}
	}

	s.mu.Lock()
	for name := range old {
		if _, exists := next[name]; !exists {
			delete(s.processes, name)
			delete(s.statuses, name)
			delete(s.cancelFuncs, name)
		}
	}
	toStart := make([]string, 0)
	for _, p := range newConfig.Processes {
		m := s.processes[p.Name]
		if m == nil {
			m = s.addProcessLocked(p)
			toStart = append(toStart, p.Name)
		} else if !reflect.DeepEqual(m.config, p) {
			m.config = p
			toStart = append(toStart, p.Name)
		}
	}
	s.Config = newConfig
	s.mu.Unlock()
	for _, name := range toStart {
		if err := s.StartProcess(name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Supervisor) Wait() { s.wg.Wait() }

func (s *Supervisor) superviseProcess(ctx context.Context, proc ProcessConfig) {
	failures := 0
	for {
		if ctx.Err() != nil {
			s.SetState(proc.Name, StateStopped)
			return
		}
		s.SetState(proc.Name, StateRunning)
		err := s.runProcess(ctx, proc)
		cancelled := ctx.Err() != nil
		failed := err != nil
		if cancelled {
			s.SetState(proc.Name, StateStopped)
			return
		}
		if failed {
			failures++
			log.Printf("[ERROR] [%s] ejecución fallida: %v", proc.Name, err)
		} else {
			failures = 0
		}
		restart := proc.RestartPolicy == "always" || (proc.RestartPolicy == "on-failure" && failed)
		if !restart {
			if failed {
				s.SetState(proc.Name, StateFailed)
			} else {
				s.SetState(proc.Name, StateStopped)
			}
			return
		}
		delay := 100 * time.Millisecond
		if failed {
			b := normalizedBackoff(proc.Backoff)
			if b.MaxTries >= 0 && failures > b.MaxTries {
				s.SetState(proc.Name, StateFailed)
				return
			}
			delay = time.Duration(float64(b.Base) * math.Pow(b.Factor, float64(failures-1)))
			if delay > b.Max {
				delay = b.Max
			}
			s.SetState(proc.Name, StateBackoff)
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			s.SetState(proc.Name, StateStopped)
			return
		}
	}
}

func normalizedBackoff(in *BackoffConfig) BackoffConfig {
	b := BackoffConfig{Base: time.Second, Factor: 2, Max: 8 * time.Second, MaxTries: 5}
	if in == nil {
		return b
	}
	if in.Base > 0 {
		b.Base = in.Base
	}
	if in.Factor > 1 {
		b.Factor = in.Factor
	}
	if in.Max > 0 {
		b.Max = in.Max
	}
	if in.MaxTries != 0 {
		b.MaxTries = in.MaxTries
	}
	return b
}

func (s *Supervisor) runProcess(ctx context.Context, proc ProcessConfig) error {
	if err := os.MkdirAll(s.LogsDir, 0755); err != nil {
		return fmt.Errorf("crear directorio de logs: %w", err)
	}
	outFile, err := os.OpenFile(filepath.Join(s.LogsDir, proc.Name+".out.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("abrir stdout: %w", err)
	}
	defer outFile.Close()
	errFile, err := os.OpenFile(filepath.Join(s.LogsDir, proc.Name+".err.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("abrir stderr: %w", err)
	}
	defer errFile.Close()

	cmd := exec.CommandContext(ctx, proc.Command, proc.Args...)
	cmd.Dir = proc.WorkingDir
	cmd.Env = os.Environ()
	for k, v := range proc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout, cmd.Stderr = outFile, errFile
	wait := proc.StopWait
	if wait == 0 {
		wait = 5 * time.Second
	}
	cmd.WaitDelay = wait
	cmd.Cancel = func() error { return terminateProcess(cmd.Process, proc.StopSignal, proc.Name) }
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("arranque: %w", err)
	}
	s.incrementStartCount(proc.Name)
	return cmd.Wait()
}
