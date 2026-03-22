package ollama

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Manager handles starting Ollama as a subprocess and pulling the model.
type Manager struct {
	BaseURL string
	Model   string
	cmd     *exec.Cmd
}

// Start launches Ollama serve as a child process if it isn't already running,
// then ensures the requested model is pulled. It blocks until ready or ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
	if m.Model == "" {
		return fmt.Errorf("ollama: no model configured")
	}

	// Check if ollama binary exists
	ollamaPath, err := exec.LookPath("ollama")
	if err != nil {
		return fmt.Errorf("ollama: binary not found in PATH (install: curl -fsSL https://ollama.com/install.sh | sh)")
	}

	host := strings.TrimPrefix(strings.TrimPrefix(m.BaseURL, "http://"), "https://")
	host = strings.TrimRight(host, "/")

	// Check if Ollama is already running
	if m.isReady() {
		log.Printf("ollama: already running at %s", m.BaseURL)
	} else {
		// Start ollama serve
		m.cmd = exec.CommandContext(ctx, ollamaPath, "serve")
		m.cmd.Env = append(os.Environ(), "OLLAMA_HOST="+host)
		m.cmd.Stdout = os.Stderr
		m.cmd.Stderr = os.Stderr
		if err := m.cmd.Start(); err != nil {
			return fmt.Errorf("ollama: failed to start: %w", err)
		}
		log.Printf("ollama: started pid %d at %s", m.cmd.Process.Pid, m.BaseURL)

		// Wait for it to be ready
		if err := m.waitReady(ctx, 30*time.Second); err != nil {
			return fmt.Errorf("ollama: not ready: %w", err)
		}
	}

	// Pull model if needed
	if err := m.ensureModel(ctx); err != nil {
		return fmt.Errorf("ollama: model pull failed: %w", err)
	}

	return nil
}

// Stop kills the Ollama subprocess if we started it.
func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		log.Printf("ollama: stopping pid %d", m.cmd.Process.Pid)
		_ = m.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_ = m.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = m.cmd.Process.Kill()
		}
	}
}

func (m *Manager) isReady() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(m.BaseURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (m *Manager) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout after %s", timeout)
		case <-tick.C:
			if m.isReady() {
				return nil
			}
		}
	}
}

func (m *Manager) ensureModel(ctx context.Context) error {
	// Check if model exists via /api/tags
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(m.BaseURL + "/api/tags")
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Try to pull — ollama pull is idempotent and fast if model exists
	log.Printf("ollama: ensuring model %s is available...", m.Model)
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(pullCtx, "ollama", "pull", m.Model)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pull %s: %w", m.Model, err)
	}
	log.Printf("ollama: model %s ready", m.Model)
	return nil
}
