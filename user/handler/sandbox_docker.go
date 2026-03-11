package handler

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"
)

type SandboxDockerManager struct {
	baseURL string
	portMin int
	portMax int
}

func NewSandboxDockerManager(baseURL string) *SandboxDockerManager {
	return &SandboxDockerManager{
		baseURL: baseURL,
		portMin: 32000,
		portMax: 32999,
	}
}

type SandboxStartResult struct {
	ContainerID string
	Port        int
	AccessURL   string
}

var sandboxTemplateImages = map[string]string{
	"openclaw":       "opencsg/openclaw-demo:latest",
	"openclaw-local": "nginx:alpine",
	"gradio-demo":    "opencsg/gradio-demo:latest",
	"jupyter":        "jupyter/base-notebook:latest",
}

func (m *SandboxDockerManager) StartContainer(ctx context.Context, instanceID int64, template string, envVars map[string]string, ttlSeconds int) (*SandboxStartResult, error) {
	image, ok := sandboxTemplateImages[template]
	if !ok {
		image = "nginx:alpine"
	}

	port := m.pickPort()
	containerName := fmt.Sprintf("sandbox_%d", instanceID)

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--rm",
		"--cpus=1",
		"--memory=512m",
		"-p", fmt.Sprintf("%d:80", port),
		"--label", fmt.Sprintf("sandbox_id=%d", instanceID),
	}
	for k, v := range envVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, image)

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run failed: %w\noutput: %s", err, string(out))
	}

	containerID := strings.TrimSpace(string(out))
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}

	return &SandboxStartResult{
		ContainerID: containerID,
		Port:        port,
		AccessURL:   fmt.Sprintf("%s:%d", m.baseURL, port),
	}, nil
}

func (m *SandboxDockerManager) StopContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop failed: %w\n%s", err, string(out))
	}
	return nil
}

func (m *SandboxDockerManager) WaitReady(ctx context.Context, port int, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "curl", "-sf", "--max-time", "1",
			fmt.Sprintf("http://localhost:%d/", port))
		if cmd.Run() == nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func (m *SandboxDockerManager) pickPort() int {
	return m.portMin + rand.Intn(m.portMax-m.portMin+1)
}
