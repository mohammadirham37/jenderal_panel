# Deploy Aplikasi Go One-Click — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Site `go-build`/`go-binary` dapat dideploy semudah Laravel — worker deployment otomatis build, restart, dan health-check aplikasi Go di belakang nginx `app-proxy`.

**Architecture:** Post-deploy hook di worker deployment (`internal/deployment`) yang mendelegasikan ke `website.Service.RunDeployHook` via interface sempit (pola `SetServingAccessRestorer` yang sudah ada). Repo pindah ke `<home>/app` (konvensi Laravel) dengan fallback legacy; unit systemd menerima `Environment=PORT` + `EnvironmentFile`; provisioning mengalokasikan port, menulis unit (tanpa start), dan meng-install toolchain Go bila belum ada.

**Tech Stack:** Go (chi, systemd via executor, SQLite), SvelteKit 5 runes + Tailwind v4.

**Spec:** `docs/superpowers/specs/2026-10-09-go-app-deploy-design.md` — executor WAJIB membaca spec ini bersama plan.

## Global Constraints

- **Non-regresi mutlak**: semua perilaku baru di-gate `AppType == "go"`; jalur PHP/Laravel/CodeIgniter/WordPress/static tidak boleh berubah (guard test di Task 9).
- Tidak menjalankan panel/mock API/browser testing — validasi statis saja: `go test ./...`, `npm run check` (0 error), `npm run build`, `npm test`.
- Semua string UI baru: kunci i18n EN **dan** ID (`web/src/lib/i18n/domains/wl.ts`, pola `wl.*`).
- `gofmt` hanya pada file yang diedit (banyak file lama belum gofmt — jangan format repo-wide).
- Semua perintah sistem lewat `internal/executor` (`RunSudo`/`RunSudoWithInput`); build command milik user dijalankan sebagai `sudo -u <webuser> -- /bin/bash -c …` (pola command preset yang sudah ada).
- Tidak ada migrasi database baru (kolom `app_*` sudah ada dari migrasi 033).
- Dependency direction: `deployment` **tidak boleh** mengimpor `internal/website` — hanya interface lokal + setter (pola yang sudah ada).
- Commit mengikuti gaya repo: `feat(websites): …`, `feat(deployment): …`, `ui(websites): …`.

## Review Focus

Lima kelas input yang paling mungkin menggigit, beserta test yang mem-pin-nya:

1. **Site non-go (laravel/codeigniter4/php) dideploy** → hook build TIDAK boleh terpanggil sama sekali. Test: Task 9 `TestDeploy_NonGoSite_DoesNotRunDeployHook`.
2. **Build gagal** → deployment `failed`, unit TIDAK direstart (binary lama tetap melayani). Test: Task 7 `TestRunDeployHook_BuildFailure_DoesNotRestart` + Task 9 `TestDeploy_HookError_MarksDeploymentFailed`.
3. **Site go lama (repo di `<home>/public`)** → semua komponen tetap memakai docroot lama. Test: Task 1 `TestAppProjectRoot_LegacyPublicGit`, dipakai bersama oleh Task 4/5/8.
4. **`AppPort == 0`** (site belum diprovision / SaveAppService belum pernah jalan) → `RunDeployHook` mengembalikan error validasi yang jelas, bukan proxy ke port 0. Test: Task 7 `TestRunDeployHook_NoPort_ValidationError`.
5. **Runtime `go-binary`** → hook melewati build tetapi tetap restart + health check; unit tetap menerima `Environment=PORT`. Test: Task 4 `TestBuildGoBinaryAppUnit_PortAndEnvFile` + Task 7 `TestRunDeployHook_GoBinary_SkipsBuildRestarts`.

---

### Task 1: `siteops.AppProjectRoot` — resolusi root repo aplikasi

**Files:**
- Create: `internal/siteops/appproject.go`
- Test: `internal/siteops/appproject_test.go`

**Interfaces:**
- Consumes: `executor.CommandExecutor` (`Run(ctx, name, args...) (*executor.Result, error)`), `executor.MockExecutor`.
- Produces: `func AppProjectRoot(ctx context.Context, exec executor.CommandExecutor, homeDir, docroot string) (root string, legacy bool)` — dipakai Task 4, 5, 8, 9.

Aturan (dari spec §1): bila `<homeDir>/public/.git` ada (probe `test -d`) → `(homeDir + "/public", true)`; selain itu → `(homeDir + "/app", false)`. Parameter `docroot` dipakai hanya sebagai fallback ketika probe error eksekusi (network/exec failure ≠ "tidak ada"): bila `Run` mengembalikan error Go (bukan exit code), kembalikan `(docroot, false)` agar konservatif.

- [ ] **Step 1: Tulis test gagal**

```go
func TestAppProjectRoot_LegacyPublicGit(t *testing.T) {
	// MockExecutor: Run("test", "-d", "/home/u/public/.git") → ExitCode 0
	// assert root == "/home/u/public", legacy == true
}
func TestAppProjectRoot_NewLayout(t *testing.T) {
	// test -d → ExitCode 1
	// assert root == "/home/u/app", legacy == false
}
```

- [ ] **Step 2: Jalankan, pastikan FAIL** — `go test ./internal/siteops/ -run TestAppProjectRoot -v` → `undefined: AppProjectRoot`.
- [ ] **Step 3: Implementasi `AppProjectRoot`** — satu probe `exec.Run(ctx, "test", "-d", homeDir+"/public/.git")`; ExitCode 0 → legacy. Gunakan `executor.MockExecutor` di test dengan `RunFunc` yang match `name == "test"`.
- [ ] **Step 4: Jalankan test → PASS.** `go test ./internal/siteops/ -v`.
- [ ] **Step 5: Commit** — `git add internal/siteops/ && git commit -m "feat(siteops): app project root resolver with legacy fallback"`

### Task 2: `goruntime.Service.InstallSync` — instal toolchain sinkron

**Files:**
- Modify: `internal/goruntime/service.go` (setelah `Install()` line ~121)
- Test: `internal/goruntime/service_test.go` (buat bila belum ada — cek dulu)

**Interfaces:**
- Consumes: `installScript() string` (sudah ada, private), `s.exec.RunSudo`.
- Produces: `func (s *Service) InstallSync(ctx context.Context, write func(string)) error` — dipakai Task 5 (`ensureGoToolchain`).

Berbeda dari `Install()` (async via task runner, return task ID): `InstallSync` menjalankan `installScript()` langsung via `s.exec.RunSudo(ctx, "bash", "-c", installScript())`, menulis stdout/stderr hasil ke `write` (dipanggil sekali di akhir dengan output lengkap), error bila exit != 0 (wrap stderr).

- [ ] **Step 1: Test gagal** — `TestInstallSync RunsScriptAndSucceeds`: MockExecutor `RunSudoFunc` mencatat argv, return ExitCode 0 + stdout "go installed" → assert err nil, argv == ["bash","-c", <script berisi "go1.27.1">], write menerima "go installed". `TestInstallSync_NonZeroExitFails`: ExitCode 1, stderr "checksum mismatch" → err != nil dan berisi "checksum mismatch".
- [ ] **Step 2: Jalankan → FAIL** (`InstallSync undefined`). Run: `go test ./internal/goruntime/ -v`.
- [ ] **Step 3: Implement `InstallSync`** — seperti deskripsi di atas; konstruksi service di test: `NewService(&executor.MockExecutor{…}, nil)`.
- [ ] **Step 4: Jalankan → PASS.**
- [ ] **Step 5: Commit** — `feat(goruntime): synchronous toolchain install for provisioning and deploy hook`

### Task 3: Default template go + kolom INSERT saat Create

**Files:**
- Modify: `internal/website/profiles.go:17-36` (struct Profile), `:132-140` (case go)
- Modify: `internal/website/service.go:319-343` (mapping Website), `:351-363` (INSERT)
- Modify: `internal/website/provisioner_test.go:19-100` (`setupTestDB` inline schema — tambah kolom `app_runtime TEXT NOT NULL DEFAULT ''` bila belum ada; `app_port`/`app_start_command`/`app_build_command` juga bila belum)
- Test: `internal/website/profiles_test.go`, `internal/website/service_test.go`

**Interfaces:**
- Produces: field Profile baru `AppRuntime`, `AppBuildCommand`, `AppStartCommand` (string, json tag `app_runtime`/`app_build_command`/`app_start_command`); nilai untuk template:
  - `go-build`: `AppRuntime: "go"`, `AppBuildCommand: "go build -o app ."`, `AppStartCommand: "./app"`, `RelativeDocumentRoot: "app/public"`
  - `go-binary`: `AppRuntime: "go-binary"`, `AppBuildCommand: ""`, `AppStartCommand: "./app"`, `RelativeDocumentRoot: "app/public"`
- Produces: `model.Website` Create mengisi `AppRuntime`/`AppStartCommand`/`AppBuildCommand` dari Profile, dan INSERT menulis kolom `app_runtime, app_start_command, app_build_command`.

- [ ] **Step 1: Test gagal** — profiles_test: `TestResolveProfileGoDefaults` (resolve `CreateRequest{Template: "go-build"}` → assert tiga field + `RelativeDocumentRoot == "app/public"`; idem `go-binary` dengan build command kosong). service_test (pola `TestCreatePersistsDerivedProfileAndCanonicalRoot`, service_test.go:237): assert row yang di-INSERT punya `app_runtime='go'`, `app_start_command='./app'`, `app_build_command='go build -o app .'`, `document_root='<home>/app/public'`.
- [ ] **Step 2: Jalankan → FAIL.** Run: `go test ./internal/website/ -run 'TestResolveProfileGo|TestCreate' -v`.
- [ ] **Step 3: Implement** — tambah field Profile + isi case go + mapping di Create + kolom INSERT + kolom inline schema test.
- [ ] **Step 4: `go test ./internal/website/ -v` → PASS semua (termasuk test lama).**
- [ ] **Step 5: Commit** — `feat(websites): go templates get repo-at-app layout and default build/start commands`

### Task 4: Unit render — `Environment=PORT`, `EnvironmentFile`, WorkingDirectory root repo

**Files:**
- Modify: `internal/website/app.go:201-228` (`buildBinaryAppUnit`), `:121-160` (`buildAppUnit`), `:88-112` (`GetAppService`), `:233-310` (`SaveAppService`)
- Test: `internal/website/app_test.go`

**Interfaces:**
- Consumes: `siteops.AppProjectRoot` (Task 1).
- Produces: `func buildBinaryAppUnit(w model.Website, projectRoot string) (string, error)` — parameter kedua berubah dari `docroot` menjadi root repo (caller yang resolve).

Perubahan `buildBinaryAppUnit` untuk `w.AppRuntime` ∈ {"go","go-binary"} (runtime deno/bun TIDAK berubah):

```ini
[Service]
User=<web_user>
WorkingDirectory=<projectRoot>
Environment=PORT=<w.AppPort>
EnvironmentFile=-<projectRoot>/.env
ExecStart=<resolve ./x atau bare command terhadap projectRoot>
Restart=always
RestartSec=5
```

`buildAppUnit` me-resolve root: `root, _ := siteops.AppProjectRoot(ctx, s.exec, homeDir, w.DocumentRoot)` — catatan: `buildAppUnit` menerima exec dari Service; bila signature-nya murni func tanpa exec, ubah jadi method `(s *Service) buildAppUnit(w, docroot)` atau tambah parameter `exec` — pilih yang paling sedikit menyentuh caller. `SaveAppService` juga memakai root hasil resolve untuk build unit. `Environment=PORT` hanya dirender bila `w.AppPort > 0`.

Perbaikan sekalian (bug ada): `GetAppService` line ~104 hardcode `Runtime: "node"` → ganti `w.AppRuntime` (default `"node"` bila kosong **kecuali** `AppType == "go"` → default `"go"`); `SaveAppService` default runtime `'node'` saat kosong juga diberi pengecualian yang sama (site go lama dengan `app_runtime=''` sembuh sendiri).

- [ ] **Step 1: Test gagal** — app_test.go:
  - `TestBuildGoAppUnit_PortEnvEnvFileAndWorkingDir`: `model.Website{AppRuntime:"go", AppPort:8210, AppStartCommand:"./app", WebUser:"u"}`, root `"/home/u/app"` → assert unit berisi `WorkingDirectory=/home/u/app`, `Environment=PORT=8210`, `EnvironmentFile=-/home/u/app/.env`, `ExecStart=/home/u/app/app`.
  - `TestBuildGoBinaryAppUnit_PortAndEnvFile`: idem runtime `"go-binary"`.
  - `TestBuildDenoAppUnit_Unchanged` (guard): runtime `"deno"`, start `"./server.js"`-style → assert TIDAK ada `Environment=PORT` dan tidak ada `EnvironmentFile` (non-regresi deno/bun).
- [ ] **Step 2: Jalankan → FAIL.** Run: `go test ./internal/website/ -run TestBuild -v`.
- [ ] **Step 3: Implement** sesuai deskripsi; sesuaikan caller `buildAppUnit`/`SaveAppService`.
- [ ] **Step 4: `go test ./internal/website/ -v` → PASS.**
- [ ] **Step 5: Commit** — `feat(websites): go app units get PORT env, optional .env, repo-root working dir`

### Task 5: Build argv benar + `ensureGoToolchain` + core `runGoBuild`

**Files:**
- Modify: `internal/website/appargs.go:37-61` (`buildAppBuildArgv` cabang go)
- Modify: `internal/website/app.go` — baru: `ensureGoToolchain`, `runGoBuild`; ubah `RunAppBuildTask:354-382` (pakai `runGoBuild`); hapus `goEnsureInstalled` (diserap).
- Test: `internal/website/app_test.go` (argv), `internal/website/service_test.go` atau app_test dengan `setupTestDB` (runGoBuild)

**Interfaces:**
- Consumes: `goruntime.Service.Status` + `InstallSync` (Task 2), `siteops.AppProjectRoot` (Task 1).
- Produces (dipakai Task 6, 7, 8):
  - `func buildAppBuildArgv(runtime, webUser, dir, buildCommand string) ([]string, error)` — cabang go kini me-return `["-u", webUser, "--", "/bin/bash", "-c", "cd <dir> && PATH=/usr/local/go/bin:$PATH <buildCommand>"]`; call site menjadi `s.exec.RunSudo(ctx, args...)` (tanpa `"-u"` ekstra — memperbaiki bug `sudo -u /bin/bash`). Cabang python tidak berubah.
  - `func (s *Service) ensureGoToolchain(ctx context.Context, write func(string)) error` — probe `goruntime.NewService(s.exec, nil).Status(ctx)`; bila `!Installed` → `write("Installing Go toolchain …")` lalu `InstallSync(ctx, write)`.
  - `func (s *Service) runGoBuild(ctx context.Context, w model.Website, root string, write func(string)) error` — (1) `test -f <root>/go.mod` via RunSudo, gagal → `model.NewValidationError("no go.mod found in the repository root; is this a Go project?")`; (2) `ensureGoToolchain`; (3) `buildAppBuildArgv(w.AppRuntime, w.WebUser, root, w.AppBuildCommand)` + `RunSudo(ctx, args...)`; stream stdout via `write`; exit != 0 → error berisi stderr.

- [ ] **Step 1: Test gagal** —
  - `TestBuildAppBuildArgvGo`: assert argv persis `["-u","u","--","/bin/bash","-c","cd /home/u/app && PATH=/usr/local/go/bin:$PATH go build -o app ."]`.
  - `TestRunGoBuild_MissingGoMod`: MockExecutor `test -f` exit 1 → err adalah ValidationError `no go.mod`.
  - `TestRunGoBuild_RunsBuildAsWebUser`: `test -f` exit 0, `go` (lewat bash) exit 0 → assert su terakhir berpola `sudo -u u -- /bin/bash -c cd /home/u/app…`; err nil.
  - `TestRunGoBuild_CompilerErrorFails`: bash exit 1 stderr `cannot find package` → err berisi string itu.
  - `TestEnsureGoToolchain_InstallsWhenMissing`: probe `go version` exit 1 → assert `bash -c <script>` terpanggil; probe exit 0 → tidak terpanggil.
- [ ] **Step 2: `go test ./internal/website/ -run 'TestBuildAppBuildArgvGo|TestRunGoBuild|TestEnsureGoToolchain' -v` → FAIL.**
- [ ] **Step 3: Implement**; `RunAppBuildTask` di-refactor memanggil `runGoBuild` lalu `AppAction("restart")` (perilaku tombol Build manual tetap).
- [ ] **Step 4: `go test ./internal/website/ -v` → PASS.**
- [ ] **Step 5: Commit** — `fix(websites): go build argv runs as web user with toolchain on PATH; auto-install toolchain`

### Task 6: `waitAppHealthy` — health check TCP

**Files:**
- Modify: `internal/website/app.go`
- Test: `internal/website/app_test.go`

**Interfaces:**
- Produces: `func (s *Service) waitAppHealthy(ctx context.Context, port int) error` — loop hingga 30×1s `net.DialTimeout("tcp", "127.0.0.1:<port>", 1*time.Second)`; sukses → nil; habis waktu → `model.NewDomainError("APP_HEALTH_CHECK_FAILED", "the app is not listening on 127.0.0.1:<port> after 30s; check the unit logs", nil)`. Dipakai Task 7.

- [ ] **Step 1: Test gagal** — `TestWaitAppHealthy_ListeningPortSucceeds`: buka `net.Listen("tcp", "127.0.0.1:0")`, ambil port, defer close → err nil. `TestWaitAppHealthy_ClosedPortTimesOut`: port ditutup; agar test cepat, inject deadline via `ctx` yang sudah hampir habis (context.WithTimeout 150ms) → err `APP_HEALTH_CHECK_FAILED` (loop menghormati ctx).
- [ ] **Step 2: → FAIL.** Run: `go test ./internal/website/ -run TestWaitAppHealthy -v`.
- [ ] **Step 3: Implement** (select ctx.Done() di loop).
- [ ] **Step 4: → PASS.**
- [ ] **Step 5: Commit** — `feat(websites): TCP health check helper for app services`

### Task 7: `website.Service.RunDeployHook`

**Files:**
- Modify: `internal/website/app.go` (atau file baru `internal/website/deployhook.go` — lebih baik file baru, satu tanggung jawab)
- Test: `internal/website/deployhook_test.go`

**Interfaces:**
- Consumes: Tasks 1, 4, 5, 6.
- Produces: `func (s *Service) RunDeployHook(ctx context.Context, websiteID string, write func(string)) error` — dipakai Task 9 via interface `deployment.deployHookRunner`. Flow:
  1. `s.Get(ctx, websiteID)`; bila `w.AppType != "go"` → return nil (no-op; guard ganda di sisi deployment).
  2. `root, _ := siteops.AppProjectRoot(ctx, s.exec, homeDir, w.DocumentRoot)`.
  3. `runtime := w.AppRuntime; if runtime == "" { runtime = "go" }`.
  4. Bila `w.AppPort <= 0` → `model.NewValidationError("app port is not allocated yet; finish provisioning first")`.
  5. Runtime `"go"` → `runGoBuild(ctx, w, root, write)` (gagal → return err, **tanpa restart**).
  6. Tentukan aksi: `systemctl is-active <unit>` exit 0 → `"restart"`, selain itu → `"start"`; jalankan `s.AppAction(ctx, websiteID, aksi)`; `write("app service " + aksi + "ed")`.
  7. `s.waitAppHealthy(ctx, w.AppPort)` → sukses: `write(fmt.Sprintf("app healthy on port %d", w.AppPort))`.

- [ ] **Step 1: Test gagal** (pola harness `setupTestDB` + `executor.MockExecutor`, website tanpa provisioner queue — ikuti pola service_test.go):
  - `TestRunDeployHook_NonGoIsNoOp`: site `app_type='php'` → err nil, executor tidak menerima command app apa pun.
  - `TestRunDeployHook_GoBuildsRestartsAndHealthChecks`: site go `app_runtime='go'`, port 8210; listener TCP nyala di 8210; MockExecutor: `test -f go.mod` 0, `go version` 0, `is-active` 0, `systemctl restart` 0 → err nil; assert urutan command mengandung `restart`; write-log berisi "app healthy on port 8210".
  - `TestRunDeployHook_BuildFailure_DoesNotRestart`: bash exit 1 → err != nil; assert TIDAK ada `systemctl restart/start` dipanggil.
  - `TestRunDeployHook_NoPort_ValidationError`: `app_port=0` → ValidationError.
  - `TestRunDeployHook_GoBinary_SkipsBuildRestarts`: `app_runtime='go-binary'`, listener nyala → tidak ada `bash -c … go build`; ada `systemctl restart`.
- [ ] **Step 2: → FAIL.** Run: `go test ./internal/website/ -run TestRunDeployHook -v`.
- [ ] **Step 3: Implement `deployhook.go`.**
- [ ] **Step 4: → PASS** dan `go test ./internal/website/ -v` tetap hijau.
- [ ] **Step 5: Commit** — `feat(websites): deploy hook builds, restarts, and health-checks go apps`

### Task 8: Provisioning — alokasi port, tulis unit (tanpa start), install toolchain

**Files:**
- Modify: `internal/website/app.go:45-85` (`allocateAppPort` → package func + method wrapper), `:233-310` (ekstrak `writeAppUnitFiles`)
- Modify: `internal/website/provisioner.go:188-469` (langkah baru), `:816-875` (`websiteRow`/`loadWebsite` — tambah `app_runtime` bila belum di-scan)
- Test: `internal/website/provisioner_test.go`

**Interfaces:**
- Consumes: Tasks 2, 4 (`buildAppUnit` via helper baru), 5 (`ensureGoToolchain`).
- Produces (dipakai provisioner dan SaveAppService):
  - `func allocateAppPort(ctx context.Context, db *sql.DB, exec executor.CommandExecutor) (int, error)` — isi body dipindah dari method; method `(s *Service) allocateAppPort` jadi wrapper.
  - `func writeAppUnitFiles(ctx context.Context, exec executor.CommandExecutor, w model.Website) error` — build unit (dengan root via `siteops.AppProjectRoot`) → `RunSudoWithInput` tee ke `/etc/systemd/system/<unit>` → `daemon-reload` → `enable` (TANPA start). Diekstrak dari `SaveAppService` step 6-7; `SaveAppService` memanggil helper ini.
  - Provision step baru `p.setupGoApp(ctx, w, progressFn)` dipanggil untuk `w.AppType == "go"` **sebelum blok render vhost** (sebelum provisioner.go:315, agar `w.AppPort` terisi saat `VhostData` dibangun): alokasikan port bila 0 → `UPDATE websites SET app_port=?, app_runtime=?` (runtime default "go" bila kosong) → set field di `w` → `writeAppUnitFiles` → untuk runtime "go": `ensureGoToolchain(ctx, progressFn)`.

- [ ] **Step 1: Test gagal** — provisioner_test (harness `setupTestDB` + MockExecutor; perlu mock yang membalas `ss`, `tee`, `systemctl`, `go version`, `nginx` dst. — perluas fixture yang ada):
  - `TestProvision_GoSite_AllocatesPortWritesUnitWithoutStart`: insert site go (app_port 0) → jalankan `provision` sinkron (pola test provisioner yang ada) → assert DB `app_port BETWEEN 8200 AND 8299` dan `app_runtime='go'`; assert `tee` ke `/etc/systemd/system/jenderal-app-<id>.service` terjadi dan isinya berisi `Environment=PORT=`; assert TIDAK ada `systemctl start jenderal-app-…`; assert vhost `sites-available` berisi `proxy_pass http://127.0.0.1:<port>`.
  - `TestProvision_LaravelSite_NoAppUnit` (guard non-regresi): site laravel → tidak ada tee unit app, tidak ada UPDATE app_port.
  - `TestProvision_GoSite_InstallsToolchainWhenMissing`: `go version` exit 1 → assert `bash -c` install script terpanggil.
- [ ] **Step 2: → FAIL.** Run: `go test ./internal/website/ -run 'TestProvision_Go|TestProvision_Laravel' -v`.
- [ ] **Step 3: Implement** — refactors + langkah baru; perhatikan provisioner perlu db (sudah ada `p.db`).
- [ ] **Step 4: `go test ./internal/website/ -v` → PASS semua.**
- [ ] **Step 5: Commit** — `feat(websites): go provisioning allocates port, writes unit, installs toolchain`

### Task 9: Integrasi worker deployment + wiring main.go

**Files:**
- Modify: `internal/deployment/service.go:20-70` (interface + field + setter), `:169-173` (SELECT + `app_type`), `:250-254` (projectRoot go), `:371-384` (panggil hook sebelum success UPDATE)
- Modify: `cmd/jenderal/main.go` (~line 323, dekat setter lain): `deploySvc.SetDeployHook(websiteSvc)`
- Test: `internal/deployment/service_test.go`

**Interfaces:**
- Consumes: `siteops.AppProjectRoot` (Task 1), `website.Service.RunDeployHook` (Task 7 — via interface, tanpa import package website).
- Produces:
  - `type deployHookRunner interface { RunDeployHook(ctx context.Context, websiteID string, write func(string)) error }` + field `deployHook deployHookRunner` + `func (s *Service) SetDeployHook(runner deployHookRunner)`.
  - SELECT ditambah `app_type`; `projectRoot`: setelah baris lift laravel yang ada, tambah `if appType == "go" { if root, _ := siteops.AppProjectRoot(ctx, s.exec, homeDir, docRoot); root != "" { projectRoot = root } }` (probe sebelum clone menentukan target; site baru → `<home>/app`).
  - Setelah blok `RestoreServingAccess` (service.go:371-375), **sebelum** UPDATE success:

```go
	if s.deployHook != nil && appType == "go" {
		if err := s.deployHook.RunDeployHook(ctx, d.WebsiteID, func(line string) {
			logBuf.WriteString(line + "\n")
		}); err != nil {
			logBuf.WriteString("ERROR: app deploy hook: " + err.Error() + "\n")
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
	}
```

- [ ] **Step 1: Test gagal** — service_test.go (pola `recordingRestorer` di serving_test.go:14-38; buat `recordingHook` serupa yang mencatat websiteID dan bisa di-set error):
  - `TestDeploy_NonGoSite_DoesNotRunDeployHook`: `insertWebsite` (app_type php) + hook recorder → deploy → assert recorder kosong, status `success` (guard non-regresi; jalankan juga varian framework `laravel` dan `codeigniter4` via update kolom framework).
  - `TestDeploy_GoSite_RunsHookAndSucceeds`: insert site `app_type='go'` (ubah helper atau insert manual) → deploy → assert hook terpanggil sekali dengan websiteID benar, status `success`, log deployment berisi output hook.
  - `TestDeploy_HookError_MarksDeploymentFailed`: hook return error "boom" → status `failed`, log berisi "app deploy hook: boom".
  - `TestDeploy_GoSite_ClonesIntoAppDir`: assert argumen `git clone`/`su` memakai `/home/<u>/app` (bukan `/home/<u>/public`).
- [ ] **Step 2: → FAIL.** Run: `go test ./internal/deployment/ -v`.
- [ ] **Step 3: Implement** + wiring main.go.
- [ ] **Step 4: `go test ./internal/deployment/ ./internal/website/ -v` → PASS.**
- [ ] **Step 5: Commit** — `feat(deployment): go sites build, restart, and health-check on deploy`

### Task 10: Frontend — tab App (PORT + snippet Fiber + hint .env)

**Files:**
- Modify: `web/src/lib/components/WebsiteAppSection.svelte` (header line 143 hardcode "Node {nodeVersion}" → label sadar-runtime; kartu baru)
- Modify: `web/src/lib/i18n/domains/wl.ts` (kunci EN+ID)
- Test: `cd web && npm run check && npm run build && npm test`

**Interfaces:**
- Consumes: `AppStatus.runtime` kini `"go"|"go-binary"` dari backend (Task 4); prop `runtime` komponen tetap diterima tapi gunakan `status.runtime` yang di-load untuk branching.

Perubahan:
- Header: bila `status.runtime` go/go-binary → tampilkan `Go` (bukan "Node {version}"); PORT tetap ditampilkan.
- Kartu info baru (hanya go/go-binary), di atas kartu status: "Aplikasi Anda harus mendengarkan PORT" + blok kode siap-salin `app.Listen(":" + os.Getenv("PORT"))` (tag `<pre><code>`, hati-hati kurung kurawal di Svelte — pakai `{'{'}` escaping atau simpan di variabel string) + hint `.env` format `KEY=val` per baris (bukan format Laravel `export`).
- Kunci i18n baru (EN + ID, wajib keduanya): `wl.appPortTitle`, `wl.appPortSnippetNote`, `wl.appEnvFileNote`.

- [ ] **Step 1: Implement UI + kunci i18n** (frontend tidak TDD — validasi statis).
- [ ] **Step 2: Verifikasi** — `cd web && npm run check` → 0 error; `npm run build` → sukses; `npm test` → pass.
- [ ] **Step 3: Commit** — `ui(websites): go app tab shows PORT, Fiber snippet, and env-file hint`

### Task 11: Frontend — badge build otomatis + hint form tambah site + validasi penuh

**Files:**
- Modify: `web/src/routes/websites/[id]/+page.svelte` (tab Deployment, area inline mulai line 1923: badge `{#if website?.app_type === 'go'}` di header kartu Git)
- Modify: `web/src/routes/websites/+page.svelte` (form tambah, setelah select template ~line 427: blok `{#if selection.template === 'go-build' || selection.template === 'go-binary'}` hint satu kalimat)
- Modify: `web/src/lib/i18n/domains/wl.ts` (`wd.deploy.autoBuild` → pindah ke domain wd.ts bila kunci Deployment hidup di sana — cek file `web/src/lib/i18n/domains/wd.ts`; `wl.goTemplateHint`)
- Test: statis.

- [ ] **Step 1: Implement** badge + hint + kunci EN/ID.
- [ ] **Step 2: Verifikasi frontend** — `npm run check` 0 error, `npm run build`, `npm test`.
- [ ] **Step 3: Validasi backend penuh** — `make test` (go test ./... -v -race) dan `make lint`; `gofmt -l` HANYA pada file yang diedit (harus kosong).
- [ ] **Step 4: Verifikasi non-regresi akhir** — `go test ./internal/deployment/ ./internal/website/ -v` semua hijau termasuk test laravel/CI4 lama.
- [ ] **Step 5: Commit** — `ui(websites): auto-build badge and go template hint on the add form`

---

## Catatan eksekusi

- Urutan task wajib: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 (rantai Interfaces).
- Setelah semua task: maintainer melakukan acceptance manual di VPS uji sesuai spec §6 (regresi Laravel+CI4, skenario inti Fiber, build rusak, webhook, go-binary) sebelum push/deploy panel.
- Jangan `git push` — maintainer yang push (alur update panel lewat halaman Update).
