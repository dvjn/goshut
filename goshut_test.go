package goshut

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
)

func TestNew(t *testing.T) {
	timeout := 5 * time.Second
	manager := New(WithTimeout(timeout))

	if manager == nil {
		t.Fatal("New() should return a valid ShutdownManager instance, got nil")
	}

	if manager.timeout != timeout {
		t.Errorf("New() should set timeout correctly: expected %v, got %v", timeout, manager.timeout)
	}

	if manager.shutdownFuncs == nil {
		t.Error("New() should initialize shutdownFuncs slice, got nil")
	}

	if len(manager.shutdownFuncs) != 0 {
		t.Errorf("New() should initialize empty shutdownFuncs slice: expected length 0, got %d", len(manager.shutdownFuncs))
	}

	if manager.exitFunc == nil {
		t.Error("New() should initialize exitFunc, got nil")
	}

	if manager.signalFunc == nil {
		t.Error("New() should initialize signalFunc, got nil")
	}
}

func TestRegister(t *testing.T) {
	manager := New(WithTimeout(5 * time.Second))

	shutdownFunc1 := func(ctx context.Context) error {
		return nil
	}

	shutdownFunc2 := func(ctx context.Context) error {
		return errors.New("shutdown error")
	}

	manager.Register(shutdownFunc1)
	if len(manager.shutdownFuncs) != 1 {
		t.Errorf("Register() should add first function: expected 1 shutdown function, got %d", len(manager.shutdownFuncs))
	}

	manager.Register(shutdownFunc2)
	if len(manager.shutdownFuncs) != 2 {
		t.Errorf("Register() should add second function: expected 2 shutdown functions, got %d", len(manager.shutdownFuncs))
	}
}

func TestShutdown(t *testing.T) {
	tests := []struct {
		name           string
		timeout        time.Duration
		shutdownFuncs  []ShutdownFunc
		expectExit     bool
		expectExitCode int
	}{
		{
			name:           "successful shutdown with no functions",
			timeout:        1 * time.Second,
			shutdownFuncs:  []ShutdownFunc{},
			expectExit:     true,
			expectExitCode: 0,
		},
		{
			name:    "successful shutdown with one function",
			timeout: 1 * time.Second,
			shutdownFuncs: []ShutdownFunc{
				func(ctx context.Context) error {
					return nil
				},
			},
			expectExit:     true,
			expectExitCode: 0,
		},
		{
			name:    "shutdown with error function",
			timeout: 1 * time.Second,
			shutdownFuncs: []ShutdownFunc{
				func(ctx context.Context) error {
					return errors.New("shutdown error")
				},
			},
			expectExit:     true,
			expectExitCode: 0,
		},
		{
			name:    "shutdown with mixed success/error functions",
			timeout: 1 * time.Second,
			shutdownFuncs: []ShutdownFunc{
				func(ctx context.Context) error {
					return nil
				},
				func(ctx context.Context) error {
					return errors.New("shutdown error")
				},
				func(ctx context.Context) error {
					return nil
				},
			},
			expectExit:     true,
			expectExitCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exitCalled := false
			exitCode := -1

			mockExit := func(code int) {
				exitCalled = true
				exitCode = code
			}

			mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
				return context.WithCancel(ctx)
			}

			manager := New(WithTimeout(tt.timeout))
			manager.exitFunc = mockExit
			manager.signalFunc = mockSignal
			for _, fn := range tt.shutdownFuncs {
				manager.Register(fn)
			}

			manager.shutdown()

			if exitCalled != tt.expectExit {
				t.Errorf("shutdown() exit behavior mismatch for test '%s': expected exitCalled %v, got %v", tt.name, tt.expectExit, exitCalled)
			}

			if tt.expectExit && exitCode != tt.expectExitCode {
				t.Errorf("shutdown() exit code mismatch for test '%s': expected %d, got %d", tt.name, tt.expectExitCode, exitCode)
			}
		})
	}
}

func TestShutdownWithTimeout(t *testing.T) {
	exitCalled := false
	exitCode := -1

	mockExit := func(code int) {
		exitCalled = true
		exitCode = code
	}

	mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}

	timeout := 100 * time.Millisecond
	manager := New(WithTimeout(timeout))
	manager.exitFunc = mockExit
	manager.signalFunc = mockSignal

	functionCalled := false
	timedOut := false

	// Register a function that takes longer than the timeout
	manager.Register(func(ctx context.Context) error {
		functionCalled = true
		select {
		case <-ctx.Done():
			timedOut = true
			return ctx.Err()
		case <-time.After(timeout * 2): // Use relative timing: 2x the timeout
			return nil
		}
	})

	manager.shutdown()

	if !exitCalled {
		t.Error("shutdown() should call exit when timeout occurs")
	}

	if exitCode != 0 {
		t.Errorf("shutdown() should exit with code 0 even on timeout: expected 0, got %d", exitCode)
	}

	if !functionCalled {
		t.Error("shutdown() should call registered shutdown function even if it times out")
	}

	if !timedOut {
		t.Error("shutdown() should timeout slow shutdown functions within the specified timeout period")
	}
}

func TestWaitForSignal(t *testing.T) {
	signalReceived := make(chan bool, 1)

	mockExit := func(code int) {
		// Don't actually exit in tests
	}

	mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		// Create a context that will be canceled when we want to simulate signal
		signalCtx, cancel := context.WithCancel(ctx)

		// Simulate receiving a signal after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		return signalCtx, cancel
	}

	manager := New(WithTimeout(1 * time.Second))
	manager.exitFunc = mockExit
	manager.signalFunc = mockSignal

	go func() {
		manager.waitForSignal()
		signalReceived <- true
	}()

	select {
	case <-signalReceived:
		// Signal was received and handled
	case <-time.After(1 * time.Second):
		t.Error("waitForSignal() should return when signal context is canceled, but timed out after 1 second")
	}
}

func TestWaitAndShutdown(t *testing.T) {
	exitCalled := false
	exitCode := -1
	shutdownFunctionCalled := false

	mockExit := func(code int) {
		exitCalled = true
		exitCode = code
	}

	mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		// Create a context that will be canceled immediately to simulate signal
		signalCtx, cancel := context.WithCancel(ctx)
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()
		return signalCtx, cancel
	}

	manager := New(WithTimeout(1 * time.Second))
	manager.exitFunc = mockExit
	manager.signalFunc = mockSignal

	manager.Register(func(ctx context.Context) error {
		shutdownFunctionCalled = true
		return nil
	})

	// Run WaitAndShutdown in a goroutine since it would normally block
	done := make(chan bool)
	go func() {
		manager.WaitAndShutdown()
		done <- true
	}()

	select {
	case <-done:
		// WaitAndShutdown completed
	case <-time.After(1 * time.Second):
		t.Fatal("WaitAndShutdown() should complete when signal is received, but timed out after 1 second")
	}

	if !exitCalled {
		t.Error("WaitAndShutdown() should call exit after shutdown completes")
	}

	if exitCode != 0 {
		t.Errorf("WaitAndShutdown() should exit with code 0 on successful shutdown: expected 0, got %d", exitCode)
	}

	if !shutdownFunctionCalled {
		t.Error("WaitAndShutdown() should execute all registered shutdown functions")
	}
}

func TestMultipleShutdownFunctions(t *testing.T) {
	exitCalled := false
	var mu sync.Mutex
	executionOrder := make([]int, 0)

	mockExit := func(code int) {
		exitCalled = true
	}

	mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}

	manager := New(WithTimeout(5 * time.Second))
	manager.exitFunc = mockExit
	manager.signalFunc = mockSignal

	manager.Register(func(ctx context.Context) error {
		mu.Lock()
		executionOrder = append(executionOrder, 1)
		mu.Unlock()
		return nil
	})

	manager.Register(func(ctx context.Context) error {
		mu.Lock()
		executionOrder = append(executionOrder, 2)
		mu.Unlock()
		return nil
	})

	manager.Register(func(ctx context.Context) error {
		mu.Lock()
		executionOrder = append(executionOrder, 3)
		mu.Unlock()
		return nil
	})

	manager.shutdown()

	if !exitCalled {
		t.Error("shutdown() should call exit after executing all shutdown functions")
	}

	mu.Lock()
	if len(executionOrder) != 3 {
		t.Errorf("shutdown() should execute all registered functions: expected 3 functions to execute, got %d", len(executionOrder))
	}

	// Check that functions executed in order
	expected := []int{1, 2, 3}
	for i, val := range executionOrder {
		if val != expected[i] {
			t.Errorf("shutdown() should execute functions in registration order: expected %v, got %v", expected, executionOrder)
			break
		}
	}
	mu.Unlock()
}

func TestShutdownContinuesAfterError(t *testing.T) {
	exitCalled := false
	executed := make([]bool, 3)

	mockExit := func(code int) {
		exitCalled = true
	}

	mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}

	manager := New(WithTimeout(1 * time.Second))
	manager.exitFunc = mockExit
	manager.signalFunc = mockSignal

	manager.Register(func(ctx context.Context) error {
		executed[0] = true
		return nil
	})

	manager.Register(func(ctx context.Context) error {
		executed[1] = true
		return errors.New("intentional error")
	})

	manager.Register(func(ctx context.Context) error {
		executed[2] = true
		return nil
	})

	manager.shutdown()

	if !exitCalled {
		t.Error("shutdown() should call exit even when some functions return errors")
	}

	// Check that all functions were executed despite one failing
	for i, exec := range executed {
		if !exec {
			t.Errorf("shutdown() should continue executing remaining functions after error: function %d was not executed", i)
		}
	}
}

func TestWithLogger(t *testing.T) {
	logger := testr.New(t)
	manager := New(WithLogger(logger))

	if manager.logger == nil {
		t.Error("WithLogger() should set the logger field, got nil")
	}

	// Verify the logger is actually the one we provided
	// We compare the underlying logger values since manager.logger is a pointer to logr.Logger
	if *manager.logger != logger {
		t.Error("WithLogger() should set the exact logger instance provided")
	}
}

func TestWithSignals(t *testing.T) {
	customSignals := []os.Signal{syscall.SIGINT, syscall.SIGUSR1}
	manager := New(WithSignals(customSignals...))

	if len(manager.signals) != len(customSignals) {
		t.Errorf("WithSignals() should set custom signals: expected %d signals, got %d", len(customSignals), len(manager.signals))
	}

	for i, signal := range customSignals {
		if manager.signals[i] != signal {
			t.Errorf("WithSignals() should set correct signals: expected %v at index %d, got %v", signal, i, manager.signals[i])
		}
	}
}

func TestLoggingFunctionality(t *testing.T) {
	t.Run("logging during waitForSignal", func(t *testing.T) {
		logger := testr.New(t)

		mockSignal := func(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
			// Create a context that will be canceled immediately to simulate signal
			signalCtx, cancel := context.WithCancel(ctx)
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
			return signalCtx, cancel
		}

		manager := New(WithLogger(logger))
		manager.signalFunc = mockSignal

		// Test that waitForSignal logs when a signal is received
		done := make(chan bool)
		go func() {
			manager.waitForSignal()
			done <- true
		}()

		select {
		case <-done:
			// waitForSignal completed - the logger would have been used
		case <-time.After(1 * time.Second):
			t.Fatal("waitForSignal() should complete when signal is received, but timed out")
		}
	})

	t.Run("logging during shutdown with errors", func(t *testing.T) {
		logger := testr.New(t)
		exitCalled := false

		mockExit := func(code int) {
			exitCalled = true
		}

		manager := New(WithLogger(logger))
		manager.exitFunc = mockExit

		// Register a function that returns an error
		manager.Register(func(ctx context.Context) error {
			return errors.New("test shutdown error")
		})

		manager.shutdown()

		if !exitCalled {
			t.Error("shutdown() should call exit even when functions return errors")
		}
	})

	t.Run("logging during successful shutdown", func(t *testing.T) {
		logger := testr.New(t)
		exitCalled := false

		mockExit := func(code int) {
			exitCalled = true
		}

		manager := New(WithLogger(logger))
		manager.exitFunc = mockExit

		// Register a successful function
		manager.Register(func(ctx context.Context) error {
			return nil
		})

		manager.shutdown()

		if !exitCalled {
			t.Error("shutdown() should call exit after successful shutdown")
		}
	})
}
