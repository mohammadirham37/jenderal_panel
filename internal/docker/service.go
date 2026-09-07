package docker

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages Docker operations by shelling out to the Docker CLI.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new Docker Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// ---------- Docker daemon management ----------

// Install installs Docker via apt-get.
func (s *Service) Install(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "apt-get", "install", "-y", "docker.io")
	if err != nil {
		return fmt.Errorf("docker install: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to install docker: "+result.Stderr, nil)
	}
	return nil
}

// Status returns the current Docker daemon status including version, running
// state, and container/image counts.
func (s *Service) Status(ctx context.Context) (*model.DockerStatus, error) {
	status := &model.DockerStatus{}

	// Check if docker is installed by running docker --version.
	verResult, verErr := s.exec.RunSudo(ctx, "docker", "--version")
	if verErr != nil {
		return status, nil // not installed, return zero-value
	}
	if verResult.ExitCode != 0 {
		return status, nil
	}

	status.Installed = true
	status.Version = parseDockerVersion(verResult.Stdout)

	// Check if Docker daemon is running via systemctl.
	sysResult, sysErr := s.exec.RunSudo(ctx, "systemctl", "is-active", "docker")
	if sysErr == nil && strings.TrimSpace(sysResult.Stdout) == "active" {
		status.Running = true
	}

	// Count containers.
	cntResult, cntErr := s.exec.RunSudo(ctx, "docker", "ps", "-a", "-q")
	if cntErr == nil && cntResult.ExitCode == 0 {
		status.Containers = countNonEmptyLines(cntResult.Stdout)
	}

	// Count images.
	imgResult, imgErr := s.exec.RunSudo(ctx, "docker", "images", "-q")
	if imgErr == nil && imgResult.ExitCode == 0 {
		status.Images = countNonEmptyLines(imgResult.Stdout)
	}

	return status, nil
}

// Start starts the Docker daemon via systemctl.
func (s *Service) Start(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "start", "docker")
	if err != nil {
		return fmt.Errorf("docker start: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to start docker: "+result.Stderr, nil)
	}
	return nil
}

// Stop stops the Docker daemon via systemctl.
func (s *Service) Stop(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "stop", "docker")
	if err != nil {
		return fmt.Errorf("docker stop: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to stop docker: "+result.Stderr, nil)
	}
	return nil
}

// Restart restarts the Docker daemon via systemctl.
func (s *Service) Restart(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "restart", "docker")
	if err != nil {
		return fmt.Errorf("docker restart: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to restart docker: "+result.Stderr, nil)
	}
	return nil
}

// ---------- Container management ----------

// ListContainers lists Docker containers. If all is true, stopped containers
// are included.
func (s *Service) ListContainers(ctx context.Context, all bool) ([]model.Container, error) {
	args := []string{
		"ps",
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}\t{{.Ports}}\t{{.CreatedAt}}",
		"--no-trunc",
	}
	if all {
		args = append(args, "-a")
	}

	result, err := s.exec.RunSudo(ctx, "docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DOCKER_ERROR", "failed to list containers: "+result.Stderr, nil)
	}

	return parseContainers(result.Stdout), nil
}

// StartContainer starts a stopped container by ID or name.
func (s *Service) StartContainer(ctx context.Context, id string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "start", id)
	if err != nil {
		return fmt.Errorf("docker start container: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to start container: "+result.Stderr, nil)
	}
	return nil
}

// StopContainer stops a running container by ID or name.
func (s *Service) StopContainer(ctx context.Context, id string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "stop", id)
	if err != nil {
		return fmt.Errorf("docker stop container: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to stop container: "+result.Stderr, nil)
	}
	return nil
}

// RestartContainer restarts a container by ID or name.
func (s *Service) RestartContainer(ctx context.Context, id string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "restart", id)
	if err != nil {
		return fmt.Errorf("docker restart container: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to restart container: "+result.Stderr, nil)
	}
	return nil
}

// RemoveContainer removes a container by ID or name.
func (s *Service) RemoveContainer(ctx context.Context, id string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "rm", "-f", id)
	if err != nil {
		return fmt.Errorf("docker rm container: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to remove container: "+result.Stderr, nil)
	}
	return nil
}

// ContainerLogs returns the last N lines of logs from a container.
func (s *Service) ContainerLogs(ctx context.Context, id string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	result, err := s.exec.RunSudo(ctx, "docker", "logs", "--tail", strconv.Itoa(lines), id)
	if err != nil {
		return "", fmt.Errorf("docker logs: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("DOCKER_ERROR", "failed to get container logs: "+result.Stderr, nil)
	}
	// Docker logs may write to stdout or stderr depending on the container.
	output := result.Stdout
	if output == "" {
		output = result.Stderr
	}
	return output, nil
}

// InspectContainer returns the raw JSON output of docker inspect for a container.
func (s *Service) InspectContainer(ctx context.Context, id string) (string, error) {
	result, err := s.exec.RunSudo(ctx, "docker", "inspect", id)
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("DOCKER_ERROR", "failed to inspect container: "+result.Stderr, nil)
	}
	return result.Stdout, nil
}

// ---------- Image management ----------

// ListImages lists all Docker images.
func (s *Service) ListImages(ctx context.Context) ([]model.DockerImage, error) {
	result, err := s.exec.RunSudo(ctx, "docker", "images",
		"--format", "{{.ID}}\t{{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}")
	if err != nil {
		return nil, fmt.Errorf("docker images: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DOCKER_ERROR", "failed to list images: "+result.Stderr, nil)
	}
	return parseImages(result.Stdout), nil
}

// PullImage pulls a Docker image by name (e.g. "nginx:latest").
func (s *Service) PullImage(ctx context.Context, name string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "pull", name)
	if err != nil {
		return fmt.Errorf("docker pull: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to pull image: "+result.Stderr, nil)
	}
	return nil
}

// RemoveImage removes a Docker image by ID or name.
func (s *Service) RemoveImage(ctx context.Context, id string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "rmi", id)
	if err != nil {
		return fmt.Errorf("docker rmi: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to remove image: "+result.Stderr, nil)
	}
	return nil
}

// ---------- Volume management ----------

// ListVolumes lists all Docker volumes.
func (s *Service) ListVolumes(ctx context.Context) ([]model.DockerVolume, error) {
	result, err := s.exec.RunSudo(ctx, "docker", "volume", "ls",
		"--format", "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}")
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DOCKER_ERROR", "failed to list volumes: "+result.Stderr, nil)
	}
	return parseVolumes(result.Stdout), nil
}

// CreateVolume creates a new Docker volume.
func (s *Service) CreateVolume(ctx context.Context, name string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "volume", "create", name)
	if err != nil {
		return fmt.Errorf("docker volume create: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to create volume: "+result.Stderr, nil)
	}
	return nil
}

// RemoveVolume removes a Docker volume by name.
func (s *Service) RemoveVolume(ctx context.Context, name string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "volume", "rm", name)
	if err != nil {
		return fmt.Errorf("docker volume rm: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to remove volume: "+result.Stderr, nil)
	}
	return nil
}

// ---------- Network management ----------

// ListNetworks lists all Docker networks.
func (s *Service) ListNetworks(ctx context.Context) ([]model.DockerNetwork, error) {
	result, err := s.exec.RunSudo(ctx, "docker", "network", "ls",
		"--format", "{{.ID}}\t{{.Name}}\t{{.Driver}}\t{{.Scope}}")
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("DOCKER_ERROR", "failed to list networks: "+result.Stderr, nil)
	}
	return parseNetworks(result.Stdout), nil
}

// CreateNetwork creates a new Docker network.
func (s *Service) CreateNetwork(ctx context.Context, name string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "network", "create", name)
	if err != nil {
		return fmt.Errorf("docker network create: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to create network: "+result.Stderr, nil)
	}
	return nil
}

// RemoveNetwork removes a Docker network by name.
func (s *Service) RemoveNetwork(ctx context.Context, name string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "network", "rm", name)
	if err != nil {
		return fmt.Errorf("docker network rm: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to remove network: "+result.Stderr, nil)
	}
	return nil
}

// ---------- Docker Compose ----------

// ComposeUp runs docker compose up -d for the given compose file path.
func (s *Service) ComposeUp(ctx context.Context, path string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "compose", "-f", path, "up", "-d")
	if err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to compose up: "+result.Stderr, nil)
	}
	return nil
}

// ComposeDown runs docker compose down for the given compose file path.
func (s *Service) ComposeDown(ctx context.Context, path string) error {
	result, err := s.exec.RunSudo(ctx, "docker", "compose", "-f", path, "down")
	if err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("DOCKER_ERROR", "failed to compose down: "+result.Stderr, nil)
	}
	return nil
}

// ComposeStatus returns the output of docker compose ps for the given
// compose file path.
func (s *Service) ComposeStatus(ctx context.Context, path string) (string, error) {
	result, err := s.exec.RunSudo(ctx, "docker", "compose", "-f", path, "ps")
	if err != nil {
		return "", fmt.Errorf("docker compose ps: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("DOCKER_ERROR", "failed to get compose status: "+result.Stderr, nil)
	}
	return result.Stdout, nil
}

// ---------- Parsing helpers ----------

// parseDockerVersion extracts the version string from "Docker version X.Y.Z, build ...".
func parseDockerVersion(output string) string {
	output = strings.TrimSpace(output)
	// Expected format: "Docker version 24.0.7, build afdd53b"
	if strings.HasPrefix(output, "Docker version ") {
		rest := strings.TrimPrefix(output, "Docker version ")
		if idx := strings.Index(rest, ","); idx != -1 {
			return rest[:idx]
		}
		return rest
	}
	return output
}

// countNonEmptyLines counts the number of non-empty lines in a string.
func countNonEmptyLines(s string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// parseContainers parses tab-separated docker ps output into Container models.
func parseContainers(output string) []model.Container {
	var containers []model.Container
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 7)
		if len(parts) < 7 {
			continue
		}
		containers = append(containers, model.Container{
			ID:      parts[0],
			Name:    parts[1],
			Image:   parts[2],
			Status:  parts[3],
			State:   parts[4],
			Ports:   parts[5],
			Created: parts[6],
		})
	}
	return containers
}

// parseImages parses tab-separated docker images output into DockerImage models.
func parseImages(output string) []model.DockerImage {
	var images []model.DockerImage
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		images = append(images, model.DockerImage{
			ID:         parts[0],
			Repository: parts[1],
			Tag:        parts[2],
			Size:       parts[3],
			Created:    parts[4],
		})
	}
	return images
}

// parseVolumes parses tab-separated docker volume ls output into DockerVolume models.
func parseVolumes(output string) []model.DockerVolume {
	var volumes []model.DockerVolume
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		volumes = append(volumes, model.DockerVolume{
			Name:       parts[0],
			Driver:     parts[1],
			Mountpoint: parts[2],
		})
	}
	return volumes
}

// parseNetworks parses tab-separated docker network ls output into DockerNetwork models.
func parseNetworks(output string) []model.DockerNetwork {
	var networks []model.DockerNetwork
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		networks = append(networks, model.DockerNetwork{
			ID:     parts[0],
			Name:   parts[1],
			Driver: parts[2],
			Scope:  parts[3],
		})
	}
	return networks
}
