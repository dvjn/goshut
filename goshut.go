package goshut

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
)

type (
	ShutdownFunc func(context.Context) error
	Option       func(*Manager)
	SignalFunc   func(context.Context, ...os.Signal) (context.Context, context.CancelFunc)
	ExitFunc     func(int)
)

type Manager struct {
	shutdownFuncs []ShutdownFunc
	timeout       time.Duration
	logger        *logr.Logger
	exitFunc      ExitFunc
	signalFunc    SignalFunc
	signals       []os.Signal
}

func New(options ...Option) *Manager {
	m := &Manager{
		shutdownFuncs: make([]ShutdownFunc, 0),
		timeout:       30 * time.Second,
		logger:        nil,
		exitFunc:      os.Exit,
		signalFunc:    signal.NotifyContext,
		signals:       []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT},
	}

	for _, option := range options {
		option(m)
	}

	return m
}

func WithTimeout(timeout time.Duration) Option {
	return func(m *Manager) {
		m.timeout = timeout
	}
}

func WithLogger(logger logr.Logger) Option {
	return func(m *Manager) {
		m.logger = &logger
	}
}

func WithSignals(signals ...os.Signal) Option {
	return func(m *Manager) {
		m.signals = signals
	}
}

func (m *Manager) Register(fn ShutdownFunc) {
	m.shutdownFuncs = append(m.shutdownFuncs, fn)
}

func (m *Manager) waitForSignal() {
	sigCtx, stop := m.signalFunc(context.Background(), m.signals...)
	defer stop()

	<-sigCtx.Done()
	if m.logger != nil {
		m.logger.Info("shutting down")
	}
}

func (m *Manager) shutdown() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	for _, shutdownFunc := range m.shutdownFuncs {
		if err := shutdownFunc(shutdownCtx); err != nil {
			if m.logger != nil {
				m.logger.Error(err, "failed to shutdown server")
			}
		}
	}

	if m.logger != nil {
		m.logger.Info("shutdown complete")
	}
	m.exitFunc(0)
}

func (m *Manager) WaitAndShutdown() {
	m.waitForSignal()
	m.shutdown()
}
