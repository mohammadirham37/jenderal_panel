package docker

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler handles Docker management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

// ---------- Docker daemon ----------

// Status returns the current Docker daemon status.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// Install installs Docker on the server.
func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	taskID := h.tasks.RunMultiple("Install Docker", [][]string{
		{"apt-get", "update", "-qq"},
		{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "docker.io"},
		{"systemctl", "enable", "docker"},
		{"systemctl", "start", "docker"},
	})

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "install_docker",
		Module: "docker",
		Target: "docker",
		Detail: "installed Docker via apt-get",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// Start starts the Docker daemon.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Start(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "start_docker",
		Module: "docker",
		Target: "docker",
		Detail: "started Docker daemon",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Stop stops the Docker daemon.
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Stop(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "stop_docker",
		Module: "docker",
		Target: "docker",
		Detail: "stopped Docker daemon",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restart restarts the Docker daemon.
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Restart(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "restart_docker",
		Module: "docker",
		Target: "docker",
		Detail: "restarted Docker daemon",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Containers ----------

// ListContainers returns all Docker containers. Use ?all=true to include
// stopped containers.
func (h *Handler) ListContainers(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"

	containers, err := h.svc.ListContainers(r.Context(), all)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, containers)
}

// StartContainer starts a container by ID.
func (h *Handler) StartContainer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.StartContainer(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "start_container",
		Module: "docker",
		Target: id,
		Detail: fmt.Sprintf("started container %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StopContainer stops a container by ID.
func (h *Handler) StopContainer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.StopContainer(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "stop_container",
		Module: "docker",
		Target: id,
		Detail: fmt.Sprintf("stopped container %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RestartContainer restarts a container by ID.
func (h *Handler) RestartContainer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.RestartContainer(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "restart_container",
		Module: "docker",
		Target: id,
		Detail: fmt.Sprintf("restarted container %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveContainer removes a container by ID.
func (h *Handler) RemoveContainer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.RemoveContainer(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "remove_container",
		Module: "docker",
		Target: id,
		Detail: fmt.Sprintf("removed container %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ContainerLogs returns the last N lines of logs from a container.
// Use ?lines=100 to control the number of lines (default 100).
func (h *Handler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			lines = n
		}
	}

	logs, err := h.svc.ContainerLogs(r.Context(), id, lines)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"logs": logs})
}

// InspectContainer returns raw JSON inspect data for a container.
func (h *Handler) InspectContainer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	data, err := h.svc.InspectContainer(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"inspect": data})
}

// ---------- Images ----------

// ListImages returns all Docker images.
func (h *Handler) ListImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.svc.ListImages(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, images)
}

// PullImage pulls a Docker image. Expects JSON body with "name" field.
func (h *Handler) PullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Name == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "image name is required")
		return
	}

	if err := h.svc.PullImage(r.Context(), req.Name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "pull_image",
		Module: "docker",
		Target: req.Name,
		Detail: fmt.Sprintf("pulled image %s", req.Name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveImage removes a Docker image by ID.
func (h *Handler) RemoveImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.RemoveImage(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "remove_image",
		Module: "docker",
		Target: id,
		Detail: fmt.Sprintf("removed image %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Volumes ----------

// ListVolumes returns all Docker volumes.
func (h *Handler) ListVolumes(w http.ResponseWriter, r *http.Request) {
	volumes, err := h.svc.ListVolumes(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, volumes)
}

// CreateVolume creates a new Docker volume. Expects JSON body with "name" field.
func (h *Handler) CreateVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Name == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "volume name is required")
		return
	}

	if err := h.svc.CreateVolume(r.Context(), req.Name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "create_volume",
		Module: "docker",
		Target: req.Name,
		Detail: fmt.Sprintf("created volume %s", req.Name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveVolume removes a Docker volume by name.
func (h *Handler) RemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.svc.RemoveVolume(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "remove_volume",
		Module: "docker",
		Target: name,
		Detail: fmt.Sprintf("removed volume %s", name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Networks ----------

// ListNetworks returns all Docker networks.
func (h *Handler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.svc.ListNetworks(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, networks)
}

// CreateNetwork creates a new Docker network. Expects JSON body with "name" field.
func (h *Handler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Name == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "network name is required")
		return
	}

	if err := h.svc.CreateNetwork(r.Context(), req.Name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "create_network",
		Module: "docker",
		Target: req.Name,
		Detail: fmt.Sprintf("created network %s", req.Name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveNetwork removes a Docker network by name.
func (h *Handler) RemoveNetwork(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if err := h.svc.RemoveNetwork(r.Context(), name); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "remove_network",
		Module: "docker",
		Target: name,
		Detail: fmt.Sprintf("removed network %s", name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Docker Compose ----------

// ComposeUp runs docker compose up for a given compose file.
// Expects JSON body with "path" field.
func (h *Handler) ComposeUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "compose file path is required")
		return
	}

	if err := h.svc.ComposeUp(r.Context(), req.Path); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "compose_up",
		Module: "docker",
		Target: req.Path,
		Detail: fmt.Sprintf("docker compose up for %s", req.Path),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ComposeDown runs docker compose down for a given compose file.
// Expects JSON body with "path" field.
func (h *Handler) ComposeDown(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "compose file path is required")
		return
	}

	if err := h.svc.ComposeDown(r.Context(), req.Path); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "compose_down",
		Module: "docker",
		Target: req.Path,
		Detail: fmt.Sprintf("docker compose down for %s", req.Path),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ComposeStatus returns the status of a docker compose project.
// Use ?path=/path/to/docker-compose.yml to specify the compose file.
func (h *Handler) ComposeStatus(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "compose file path is required")
		return
	}

	output, err := h.svc.ComposeStatus(r.Context(), path)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"output": output})
}
