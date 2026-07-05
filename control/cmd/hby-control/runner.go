package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func runDirect(cfg config, cmdline []string) int {
	if runtime.GOOS != "windows" {
		if err := os.Chdir(cfg.Root); err != nil {
			log.Printf("hby-control: %v", err)
			return 1
		}
		resolved, err := exec.LookPath(cmdline[0])
		if err != nil {
			log.Printf("hby-control: %v", err)
			return 127
		}
		if err := syscall.Exec(resolved, cmdline, os.Environ()); err != nil {
			log.Printf("hby-control: %v", err)
			return 1
		}
		return 0
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Dir = cfg.Root
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		log.Printf("hby-control: %v", err)
		return 1
	}
	return 0
}

func runSupervisor(cfg config, cmdline []string) int {
	supervisor := newSupervisor(cmdline, cfg.Root, cfg.StopTimeout, cfg.LogBytes)
	app := newApp(cfg, supervisor)

	if len(cmdline) > 0 && cfg.AutoStart {
		if err := supervisor.Start(); err != nil {
			log.Printf("hby-control: unable to start server command: %v", err)
		}
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		proto := "http"
		if cfg.TLSCertFile != "" {
			proto = "https"
		}
		log.Printf("hby-control: listening on %s://%s, root=%s", proto, cfg.Addr, cfg.Root)
		if cfg.TLSCertFile != "" {
			errCh <- srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("hby-control: received %s, shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("hby-control: http server failed: %v", err)
			return 1
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	_ = supervisor.Stop()
	return 0
}
