package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type supervisor struct {
	mu          sync.Mutex
	cmdline     []string
	dir         string
	stopTimeout time.Duration
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	cleanup     func()
	waitCh      chan struct{}
	running     bool
	startedAt   time.Time
	stoppedAt   time.Time
	exitCode    int
	exitError   string
	logs        *logHub
}

func newSupervisor(cmdline []string, dir string, stopTimeout time.Duration, maxLogBytes int) *supervisor {
	return &supervisor{
		cmdline:     append([]string(nil), cmdline...),
		dir:         dir,
		stopTimeout: stopTimeout,
		exitCode:    -1,
		logs:        newLogHub(maxLogBytes),
	}
}

func (s *supervisor) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.cmdline) == 0 {
		return errors.New("no server command configured")
	}
	if s.running {
		return errors.New("server command is already running")
	}

	cmd := exec.Command(s.cmdline[0], s.cmdline[1:]...)
	cmd.Dir = s.dir
	cmd.Env = os.Environ()

	processIO, err := setupProcessIO(cmd)
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		processIO.cleanup()
		return err
	}
	processIO.afterStart()

	waitCh := make(chan struct{})
	s.cmd = cmd
	s.stdin = processIO.stdin
	s.cleanup = processIO.cleanup
	s.waitCh = waitCh
	s.running = true
	s.startedAt = time.Now()
	s.stoppedAt = time.Time{}
	s.exitCode = -1
	s.exitError = ""
	s.logs.Append([]byte(fmt.Sprintf("\n[hby-control] started server: %s\n", shellJoin(s.cmdline))))

	for _, reader := range processIO.readers {
		go s.copyOutput(reader)
	}
	go s.wait(cmd, waitCh)
	return nil
}

func (s *supervisor) Stop() error {
	s.mu.Lock()
	if !s.running || s.cmd == nil || s.cmd.Process == nil {
		s.mu.Unlock()
		return nil
	}
	cmd := s.cmd
	waitCh := s.waitCh
	s.mu.Unlock()

	s.logs.Append([]byte("\n[hby-control] stopping server\n"))
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Signal(os.Interrupt)
	} else {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}

	select {
	case <-waitCh:
		return nil
	case <-time.After(s.stopTimeout):
		s.logs.Append([]byte("\n[hby-control] stop timeout reached, killing server\n"))
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		<-waitCh
		return nil
	}
}

func (s *supervisor) Restart() error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start()
}

func (s *supervisor) SendInput(data string) error {
	s.mu.Lock()
	stdin := s.stdin
	running := s.running
	s.mu.Unlock()

	if !running || stdin == nil {
		return errors.New("server command is not running")
	}
	_, err := io.WriteString(stdin, data)
	return err
}

func (s *supervisor) Status() processStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return processStatus{
		Configured: len(s.cmdline) > 0,
		Running:    s.running,
		Command:    shellJoin(s.cmdline),
		StartedAt:  s.startedAt,
		StoppedAt:  s.stoppedAt,
		ExitCode:   s.exitCode,
		ExitError:  s.exitError,
	}
}

func (s *supervisor) copyOutput(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.logs.Append(chunk)
		}
		if err != nil {
			return
		}
	}
}

func (s *supervisor) wait(cmd *exec.Cmd, waitCh chan struct{}) {
	err := cmd.Wait()
	exitCode := 0
	exitText := ""
	if err != nil {
		exitText = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	s.mu.Lock()
	var cleanup func()
	if s.cmd == cmd {
		cleanup = s.cleanup
		s.running = false
		s.cmd = nil
		s.stdin = nil
		s.cleanup = nil
		s.stoppedAt = time.Now()
		s.exitCode = exitCode
		s.exitError = exitText
	}
	s.mu.Unlock()

	if cleanup != nil {
		cleanup()
	}
	s.logs.Append([]byte(fmt.Sprintf("\n[hby-control] server exited with code %d\n", exitCode)))
	close(waitCh)
}

type processIO struct {
	stdin      io.WriteCloser
	readers    []io.Reader
	afterStart func()
	cleanup    func()
}

func setupPipeProcessIO(cmd *exec.Cmd) (*processIO, error) {
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	return &processIO{
		stdin:      stdin,
		readers:    []io.Reader{stdout, stderr},
		afterStart: func() {},
		cleanup:    func() {},
	}, nil
}

type processStatus struct {
	Configured bool      `json:"configured"`
	Running    bool      `json:"running"`
	Command    string    `json:"command"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	StoppedAt  time.Time `json:"stoppedAt,omitempty"`
	ExitCode   int       `json:"exitCode"`
	ExitError  string    `json:"exitError,omitempty"`
}

type logHub struct {
	mu   sync.Mutex
	max  int
	buf  []byte
	subs map[chan logEvent]struct{}
}

type logEvent struct {
	Type string
	Data []byte
}

func newLogHub(max int) *logHub {
	if max <= 0 {
		max = 512 * 1024
	}
	return &logHub{max: max, subs: map[chan logEvent]struct{}{}}
}

func (h *logHub) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	h.mu.Lock()
	h.buf = append(h.buf, data...)
	if len(h.buf) > h.max {
		h.buf = append([]byte(nil), h.buf[len(h.buf)-h.max:]...)
	}
	for ch := range h.subs {
		select {
		case ch <- logEvent{Type: "output", Data: append([]byte(nil), data...)}:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *logHub) Clear() {
	h.mu.Lock()
	h.buf = nil
	for ch := range h.subs {
		select {
		case ch <- logEvent{Type: "clear"}:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *logHub) Snapshot() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.buf...)
}

func (h *logHub) Subscribe() (chan logEvent, func()) {
	ch := make(chan logEvent, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}
}
