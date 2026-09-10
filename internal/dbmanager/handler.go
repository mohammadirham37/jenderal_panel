package dbmanager

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

// ─── Database management (phpMyAdmin-like) ────────────────────────

// manageTokenFromRequest extracts the management session token header.
func manageTokenFromRequest(r *http.Request) string {
	return r.Header.Get("X-DB-Manage-Token")
}

// UnlockManage handles POST /databases/users/{id}/manage/unlock.
func (h *Handler) UnlockManage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Password string `json:"password"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	panelUser, _ := auth.UserFromContext(r.Context())
	panelUserID := ""
	if panelUser.ID != "" {
		panelUserID = panelUser.ID
	}

	token, engine, username, err := h.svc.UnlockManage(r.Context(), id, req.Password, panelUserID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.audit.Log(r.Context(), audit.LogEntry{
		UserID: panelUserID,
		Action: "db_manage_unlock",
		Module: "dbmanager",
		Target: username,
		Detail: "unlocked database management session",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{
		"token":    token,
		"engine":   engine,
		"username": username,
	})
}

// LockManage handles POST /databases/manage/{token}/lock.
func (h *Handler) LockManage(w http.ResponseWriter, r *http.Request) {
	h.svc.CloseManage(manageTokenFromRequest(r))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ManageDatabases handles GET /databases/manage/{token}/databases.
func (h *Handler) ManageDatabases(w http.ResponseWriter, r *http.Request) {
	names, err := h.svc.ManageDatabases(r.Context(), manageTokenFromRequest(r))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, names)
}

// ManageTables handles GET /databases/manage/{token}/tables?database=.
func (h *Handler) ManageTables(w http.ResponseWriter, r *http.Request) {
	tables, err := h.svc.ManageTables(r.Context(), manageTokenFromRequest(r), r.URL.Query().Get("database"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, tables)
}

// ManageStructure handles GET /databases/manage/{token}/structure?database=&table=.
func (h *Handler) ManageStructure(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	columns, err := h.svc.ManageStructure(r.Context(), manageTokenFromRequest(r), query.Get("database"), query.Get("table"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, columns)
}

// ManageRows handles GET /databases/manage/{token}/rows.
func (h *Handler) ManageRows(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	perPage, _ := strconv.Atoi(query.Get("per_page"))
	rows, err := h.svc.ManageRows(r.Context(), manageTokenFromRequest(r), query.Get("database"), query.Get("table"), RowsQuery{
		Page:    page,
		PerPage: perPage,
		Sort:    query.Get("sort"),
		Desc:    query.Get("dir") == "desc",
		Search:  query.Get("search"),
	})
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, rows)
}

// ManageQuery handles POST /databases/manage/{token}/query.
func (h *Handler) ManageQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Database string `json:"database"`
		SQL      string `json:"sql"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	result, err := h.svc.ManageQuery(r.Context(), manageTokenFromRequest(r), req.Database, req.SQL)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, result)
}

// ManageDropTable handles POST /databases/manage/{token}/drop-table.
func (h *Handler) ManageDropTable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Database string `json:"database"`
		Table    string `json:"table"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.ManageDropTable(r.Context(), manageTokenFromRequest(r), req.Database, req.Table); err != nil {
		httputil.HandleError(w, err)
		return
	}

	panelUser, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: panelUser.ID,
		Action: "db_manage_drop_table",
		Module: "dbmanager",
		Target: fmt.Sprintf("%s.%s", req.Database, req.Table),
		Detail: "dropped table via database manager",
		IP:     r.RemoteAddr,
	})
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ManageEmptyTable handles POST /databases/manage/{token}/empty-table.
func (h *Handler) ManageEmptyTable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Database string `json:"database"`
		Table    string `json:"table"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.ManageEmptyTable(r.Context(), manageTokenFromRequest(r), req.Database, req.Table); err != nil {
		httputil.HandleError(w, err)
		return
	}

	panelUser, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: panelUser.ID,
		Action: "db_manage_empty_table",
		Module: "dbmanager",
		Target: fmt.Sprintf("%s.%s", req.Database, req.Table),
		Detail: "emptied table via database manager",
		IP:     r.RemoteAddr,
	})
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
