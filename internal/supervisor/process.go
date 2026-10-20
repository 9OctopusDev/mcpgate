package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type State string

const (
	StateStopped     State = "STOPPED"
	StateStarting    State = "STARTING"
	StateDiscovering State = "DISCOVERING"
	StateReady       State = "READY"
	StateFailed      State = "FAILED"
	StateBackoff     State = "BACKOFF"
	StateDraining    State = "DRAINING"
	StateCrashed     State = "CRASHED"
)

type Config struct {
	Executable      string
	Args            []string
	CWD             string
	Env             map[string]string
	MaxProcesses    int
	MaxInflight     int
	MaxFrameBytes   int
	StartupAttempts int
	Grace           time.Duration
}

func (config Config) Validate() error {
	if !filepath.IsAbs(config.Executable) || !filepath.IsAbs(config.CWD) {
		return errors.New("stdio executable and cwd must be absolute")
	}
	base := filepath.Base(config.Executable)
	if base == "sh" || base == "bash" || base == "dash" || base == "zsh" {
		return errors.New("shell executables are forbidden")
	}
	for _, argument := range config.Args {
		if strings.ContainsAny(argument, "`\n\r") || strings.Contains(argument, "$(") {
			return errors.New("stdio arguments contain shell substitution")
		}
	}
	for key := range config.Env {
		if key == "LD_PRELOAD" || key == "LD_LIBRARY_PATH" || strings.HasPrefix(key, "DYLD_") || strings.ContainsAny(key, "=\x00") {
			return errors.New("unsafe child environment")
		}
	}
	if config.MaxProcesses <= 0 || config.MaxInflight <= 0 || config.MaxFrameBytes <= 0 || config.StartupAttempts <= 0 || config.Grace <= 0 {
		return errors.New("invalid stdio process limits")
	}
	return nil
}

type callResult struct {
	frame []byte
	err   error
}
type Process struct {
	config        Config
	command       *exec.Cmd
	stdin         io.WriteCloser
	writer        *FrameWriter
	state         atomic.Value
	pendingMu     sync.Mutex
	pending       map[string]chan callResult
	semaphore     chan struct{}
	waitDone      chan struct{}
	waitErr       error
	waitMu        sync.Mutex
	terminateOnce sync.Once
}

func startProcess(config Config) (*Process, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	command := exec.Command(config.Executable, config.Args...)
	command.Dir = config.CWD
	command.Env = make([]string, 0, len(config.Env))
	for key, value := range config.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	process := &Process{config: config, command: command, stdin: stdin, writer: NewFrameWriter(stdin, config.MaxFrameBytes), pending: make(map[string]chan callResult), semaphore: make(chan struct{}, config.MaxInflight), waitDone: make(chan struct{})}
	process.state.Store(StateStarting)
	if err := command.Start(); err != nil {
		process.state.Store(StateFailed)
		return nil, err
	}
	process.state.Store(StateReady)
	go process.readLoop(stdout)
	go io.Copy(io.Discard, stderr)
	go func() {
		err := command.Wait()
		process.waitMu.Lock()
		process.waitErr = err
		process.waitMu.Unlock()
		process.state.Store(StateCrashed)
		process.failAll(errors.New("stdio process exited"))
		close(process.waitDone)
	}()
	return process, nil
}

func (process *Process) State() State { value, _ := process.state.Load().(State); return value }

func (process *Process) Call(ctx context.Context, id json.RawMessage, frame []byte) ([]byte, error) {
	if process.State() != StateReady {
		return nil, errors.New("stdio process not ready")
	}
	key := compactID(id)
	if key == "" {
		return nil, errors.New("stdio request id required")
	}
	select {
	case process.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-process.semaphore }()
	result := make(chan callResult, 1)
	process.pendingMu.Lock()
	if _, exists := process.pending[key]; exists {
		process.pendingMu.Unlock()
		return nil, errors.New("duplicate stdio request id")
	}
	process.pending[key] = result
	process.pendingMu.Unlock()
	if err := process.writer.Write(frame); err != nil {
		process.removePending(key)
		return nil, err
	}
	select {
	case completed := <-result:
		return completed.frame, completed.err
	case <-ctx.Done():
		process.removePending(key)
		cancellation, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": json.RawMessage(id)}})
		_ = process.writer.Write(cancellation)
		if process.config.MaxInflight == 1 {
			go func() {
				timer := time.NewTimer(process.config.Grace)
				defer timer.Stop()
				select {
				case <-process.waitDone:
				case <-timer.C:
					process.terminate()
				}
			}()
		}
		return nil, ctx.Err()
	}
}

func (process *Process) readLoop(stdout io.Reader) {
	reader := NewFrameReader(stdout, process.config.MaxFrameBytes)
	for {
		frame, err := reader.Read()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				process.failAll(err)
				process.terminate()
			}
			return
		}
		var message struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(frame, &message) != nil || len(message.ID) == 0 {
			process.failAll(ErrMalformedFrame)
			process.terminate()
			return
		}
		key := compactID(message.ID)
		process.pendingMu.Lock()
		channel := process.pending[key]
		delete(process.pending, key)
		process.pendingMu.Unlock()
		if channel != nil {
			channel <- callResult{frame: frame}
		}
	}
}

func (process *Process) removePending(key string) {
	process.pendingMu.Lock()
	delete(process.pending, key)
	process.pendingMu.Unlock()
}
func (process *Process) failAll(err error) {
	process.pendingMu.Lock()
	pending := process.pending
	process.pending = make(map[string]chan callResult)
	process.pendingMu.Unlock()
	for _, channel := range pending {
		channel <- callResult{err: err}
	}
}
func (process *Process) terminate() {
	process.terminateOnce.Do(func() {
		_ = process.stdin.Close()
		if process.command.Process != nil {
			_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
		}
	})
}
func (process *Process) Drain(ctx context.Context) error {
	process.state.Store(StateDraining)
	_ = process.stdin.Close()
	timer := time.NewTimer(process.config.Grace)
	defer timer.Stop()
	select {
	case <-process.waitDone:
		process.state.Store(StateStopped)
		return nil
	case <-ctx.Done():
		process.terminate()
		return ctx.Err()
	case <-timer.C:
		if process.command.Process != nil {
			_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGTERM)
		}
	}
	timer.Reset(process.config.Grace)
	select {
	case <-process.waitDone:
		process.state.Store(StateStopped)
		return nil
	case <-ctx.Done():
		process.terminate()
		return ctx.Err()
	case <-timer.C:
		process.terminate()
		<-process.waitDone
		process.state.Store(StateStopped)
		return fmt.Errorf("stdio process killed after drain timeout")
	}
}
func compactID(id json.RawMessage) string {
	var buffer bytes.Buffer
	if json.Compact(&buffer, id) != nil {
		return ""
	}
	return buffer.String()
}
