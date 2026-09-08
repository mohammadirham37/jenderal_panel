package dbmanager

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// Handler handles database management HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewHandler creates a new database management Handler.
func NewHandler(svc *Service, auditSvc *audit.Service, tasks *taskrunner.Runner) *Handler {
	return &Handler{svc: svc, audit: auditSvc, tasks: tasks}
}

// ListEngines returns the status of all database engines.
func (h *Handler) ListEngines(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.svc.ListEngines(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, statuses)
}

// InstallEngine installs a database engine via background task.
func (h *Handler) InstallEngine(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")

	var pkg string
	switch engine {
	case "mysql":
		pkg = "mysql-server"
	case "postgresql":
		pkg = "postgresql"
	case "redis":
		pkg = "redis-server"
	default:
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "unknown engine: "+engine)
		return
	}

	taskID := h.tasks.RunMultiple("Install "+engine, [][]string{
		{"apt-get", "update", "-qq"},
		{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", pkg},
	})

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "install_engine",
		Module: "dbmanager",
		Target: engine,
		Detail: "task:" + taskID,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusAccepted, map[string]string{"task_id": taskID})
}

// StartEngine starts a database engine service.
func (h *Handler) StartEngine(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")

	if err := h.svc.StartEngine(r.Context(), engine); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "start_engine",
		Module: "dbmanager",
		Target: engine,
		Detail: fmt.Sprintf("started database engine %s", engine),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// StopEngine stops a database engine service.
func (h *Handler) StopEngine(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")

	if err := h.svc.StopEngine(r.Context(), engine); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "stop_engine",
		Module: "dbmanager",
		Target: engine,
		Detail: fmt.Sprintf("stopped database engine %s", engine),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RestartEngine restarts a database engine service.
func (h *Handler) RestartEngine(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")

	if err := h.svc.RestartEngine(r.Context(), engine); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "restart_engine",
		Module: "dbmanager",
		Target: engine,
		Detail: fmt.Sprintf("restarted database engine %s", engine),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// createDatabaseRequest is the JSON body for creating a database.
type createDatabaseRequest struct {
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Charset string `json:"charset"`
}

// ListDatabases returns all managed databases.
func (h *Handler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := h.svc.ListDatabases(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, dbs)
}

// CreateDatabase creates a new managed database.
func (h *Handler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req createDatabaseRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	mdb, err := h.svc.CreateDatabase(r.Context(), req.Name, req.Engine, req.Charset)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "create_database",
		Module: "dbmanager",
		Target: req.Name,
		Detail: fmt.Sprintf("created %s database %s", req.Engine, req.Name),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusCreated, mdb)
}

// DropDatabase drops a managed database by ID.
func (h *Handler) DropDatabase(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DropDatabase(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "drop_database",
		Module: "dbmanager",
		Target: id,
		Detail: fmt.Sprintf("dropped database %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// createDBUserRequest is the JSON body for creating a database user.
type createDBUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Engine   string `json:"engine"`
}

// ListDBUsers returns all database users.
func (h *Handler) ListDBUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListDBUsers(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, users)
}

// CreateDBUser creates a new database user.
func (h *Handler) CreateDBUser(w http.ResponseWriter, r *http.Request) {
	var req createDBUserRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	u, err := h.svc.CreateDBUser(r.Context(), req.Username, req.Password, req.Engine)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "create_db_user",
		Module: "dbmanager",
		Target: req.Username,
		Detail: fmt.Sprintf("created %s user %s", req.Engine, req.Username),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusCreated, u)
}

// DropDBUser drops a database user by ID.
func (h *Handler) DropDBUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.svc.DropDBUser(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "drop_db_user",
		Module: "dbmanager",
		Target: id,
		Detail: fmt.Sprintf("dropped database user %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// resetPasswordRequest is the JSON body for resetting a user password.
type resetPasswordRequest struct {
	Password string `json:"password"`
}

// ResetPassword resets the password for a database user.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req resetPasswordRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.ResetPassword(r.Context(), id, req.Password); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "reset_db_password",
		Module: "dbmanager",
		Target: id,
		Detail: fmt.Sprintf("reset password for database user %s", id),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// grantPrivilegesRequest is the JSON body for granting privileges.
type grantPrivilegesRequest struct {
	UserID     string `json:"user_id"`
	DatabaseID string `json:"database_id"`
}

// GrantPrivileges grants privileges on a database to a user.
func (h *Handler) GrantPrivileges(w http.ResponseWriter, r *http.Request) {
	var req grantPrivilegesRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.GrantPrivileges(r.Context(), req.UserID, req.DatabaseID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "grant_privileges",
		Module: "dbmanager",
		Target: fmt.Sprintf("user=%s db=%s", req.UserID, req.DatabaseID),
		Detail: fmt.Sprintf("granted privileges on database %s to user %s", req.DatabaseID, req.UserID),
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
