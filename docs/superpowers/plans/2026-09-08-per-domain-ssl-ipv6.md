# Per-Domain SSL and Automatic IPv6 Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make panel-generated Nginx configuration work on IPv4-only and dual-stack VPS hosts, and provide secure per-domain Let's Encrypt or custom SSL installation with automatic HTTPS redirect.

**Architecture:** A shared Nginx capability probe feeds explicit IPv6 render flags into website HTTP and HTTPS templates. The SSL service validates domain ownership and certificate material, then uses one rollback-capable activation pipeline for Let's Encrypt and custom certificates; the existing SSL page becomes the single management UI.

**Tech Stack:** Go 1.24, `net/http`, `crypto/tls`, `crypto/x509`, SQLite, Nginx, Svelte 5, TypeScript/JavaScript, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-08-per-domain-ssl-ipv6-design.md`

## Global Constraints

- Ubuntu 24.04 and IPv4-only kernels must work without manual Nginx edits.
- Emit IPv6 listeners only when `/proc/net/if_inet6` contains an interface entry.
- Only primary or alias domains registered to the selected website may receive certificates.
- Custom private keys never enter SQLite, API responses, audit details, browser storage, or error messages.
- Custom SSL uses pasted Certificate / Full Chain PEM and Private Key PEM textareas.
- Let's Encrypt defaults to auto-renew; custom certificates never auto-renew.
- HTTP redirects begin only after certificate validation, `nginx -t`, and reload succeed.
- `/.well-known/acme-challenge/` remains available over HTTP.
- Failed replacement restores the previously working files, configuration, database state, and Nginx reload.
- No database migration is introduced.
- Keep existing list/get/renew/revoke/delete SSL routes backward compatible.

---

### Task 1: Detect IPv6 and Render Compatible Website Configurations

**Files:**
- Create: `internal/nginx/capability.go`
- Create: `internal/nginx/capability_test.go`
- Modify: `internal/website/templates.go`
- Modify: `internal/website/templates_test.go`
- Modify: `internal/website/provisioner.go`
- Modify: `internal/website/provisioner_test.go`

**Interfaces:**
- Produces: `nginx.IPv6Available() bool`
- Produces: `website.VhostData.IPv6 bool`
- Produces: `website.VhostData.RedirectDomains []string`
- Produces: `website.TLSVhostData` and `website.RenderTLSVhost(TLSVhostData) (string, error)`
- Consumes: existing `website.RenderVhost(VhostData) (string, error)` and provisioner domain data.

- [ ] **Step 1: Add failing capability tests**

Create table-driven tests against a controlled probe path:

```go
func TestIPv6AvailableAt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		missing bool
		want    bool
	}{
		{name: "interface present", content: "00000000000000000000000000000001 01 80 10 80 lo\n", want: true},
		{name: "disabled", content: "\n", want: false},
		{name: "probe unavailable", missing: true, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "if_inet6")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := ipv6AvailableAt(path); got != tt.want {
				t.Fatalf("ipv6AvailableAt() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the capability test and verify RED**

Run: `go test ./internal/nginx -run TestIPv6AvailableAt -count=1`

Expected: build failure because `ipv6AvailableAt` does not exist.

- [ ] **Step 3: Implement the capability probe**

Create the production wrapper and testable helper:

```go
const ipv6InterfacesPath = "/proc/net/if_inet6"

func IPv6Available() bool {
	return ipv6AvailableAt(ipv6InterfacesPath)
}

func ipv6AvailableAt(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && len(bytes.TrimSpace(data)) > 0
}
```

- [ ] **Step 4: Add failing HTTP/HTTPS render tests**

Extend `templates_test.go` with literal behavior checks:

```go
func TestRenderVhost_OmitsIPv6WhenUnavailable(t *testing.T) {
	output, err := RenderVhost(VhostData{Domain: "example.com", DocumentRoot: "/srv/example", LogDir: "/var/log/example", AppType: "static", IPv6: false})
	if err != nil { t.Fatal(err) }
	if strings.Contains(output, "listen [::]:80;") { t.Fatalf("unexpected IPv6 listener:\n%s", output) }
}

func TestRenderVhost_IncludesIPv6WhenAvailable(t *testing.T) {
	output, err := RenderVhost(VhostData{Domain: "example.com", DocumentRoot: "/srv/example", LogDir: "/var/log/example", AppType: "static", IPv6: true})
	if err != nil { t.Fatal(err) }
	if !strings.Contains(output, "listen [::]:80;") { t.Fatalf("missing IPv6 listener:\n%s", output) }
}

func TestRenderTLSVhost_RoutesAliasThroughPrimaryPHPPool(t *testing.T) {
	output, err := RenderTLSVhost(TLSVhostData{
		VhostData: VhostData{Domain: "example.com", DocumentRoot: "/srv/example", LogDir: "/var/log/example", PHPVersion: "8.3", AppType: "php", IPv6: false},
		TLSDomain: "www.example.com", CertificatePath: "/etc/jenderal/ssl/www.example.com/cert.pem", PrivateKeyPath: "/etc/jenderal/ssl/www.example.com/key.pem",
	})
	if err != nil { t.Fatal(err) }
	checks := []string{"listen 443 ssl;", "server_name www.example.com;", "php8.3-fpm-example.com.sock"}
	for _, check := range checks {
		if !strings.Contains(output, check) { t.Errorf("missing %q:\n%s", check, output) }
	}
	if strings.Contains(output, "listen [::]:443") { t.Fatalf("unexpected IPv6 listener:\n%s", output) }
}

func TestRenderVhost_SeparatesHTTPSRedirectDomains(t *testing.T) {
	output, err := RenderVhost(VhostData{Domain: "example.com", Aliases: "www.example.com api.example.com", DocumentRoot: "/srv/example", LogDir: "/var/log/example", AppType: "static", RedirectDomains: []string{"www.example.com"}})
	if err != nil { t.Fatal(err) }
	for _, check := range []string{"server_name example.com api.example.com;", "server_name www.example.com;", "location ^~ /.well-known/acme-challenge/", "return 301 https://$host$request_uri;"} {
		if !strings.Contains(output, check) { t.Errorf("missing %q:\n%s", check, output) }
	}
}
```

- [ ] **Step 5: Run render tests and verify RED**

Run: `go test ./internal/website -run 'TestRenderVhost_(Omits|Includes|Separates)|TestRenderTLSVhost' -count=1`

Expected: build failures for missing fields/types/functions, followed by assertion failures until unconditional IPv6 listeners are removed.

- [ ] **Step 6: Extend the renderer with explicit capabilities**

Add the public render data while retaining current fields:

```go
type VhostData struct {
	Domain          string
	Aliases         string
	DocumentRoot    string
	LogDir          string
	PHPVersion      string
	AppType         string
	IPv6            bool
	RedirectDomains []string
}

type TLSVhostData struct {
	VhostData
	TLSDomain       string
	CertificatePath string
	PrivateKeyPath  string
}
```

Update the templates so IPv6 lines use `{{ if .IPv6 }}`. Before executing the
HTTP template, split `Domain + " " + Aliases`, exclude exact names present in
`RedirectDomains`, join the remaining names, and expose them as
`ApplicationDomains`. Render no HTTP application server when that list is
empty. Render one HTTP redirect server per redirect domain with these blocks:

```nginx
location ^~ /.well-known/acme-challenge/ {
    root {{ $.DocumentRoot }};
    try_files $uri =404;
}

location / {
    return 301 https://$host$request_uri;
}
```

Implement `RenderTLSVhost` with the same PHP/static routing as the HTTP
application server. Its `server_name` is `TLSDomain`, while its PHP socket uses
the embedded primary `VhostData.Domain`.

- [ ] **Step 7: Pass the detected capability during provisioning**

Alias the package to avoid ambiguity and populate the field:

```go
import nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"

vhostData := VhostData{
	Domain: w.Domain, Aliases: strings.Join(aliases, " "),
	DocumentRoot: w.DocumentRoot, LogDir: logDir,
	PHPVersion: w.PHPVersion, AppType: w.AppType,
	IPv6: nginxconfig.IPv6Available(),
}
```

Update the successful provisioner test to capture the temporary vhost passed
to `cp` and verify it omits `[::]` when the test host probe reports unavailable.

- [ ] **Step 8: Run affected tests and commit**

Run: `go test ./internal/nginx ./internal/website -count=1`

Expected: PASS.

```bash
git add internal/nginx/capability.go internal/nginx/capability_test.go internal/website/templates.go internal/website/templates_test.go internal/website/provisioner.go internal/website/provisioner_test.go
git commit -m "fix(nginx): detect IPv6 before rendering listeners"
```

---

### Task 2: Validate Certificate Material and Parse Real Expiration

**Files:**
- Create: `internal/ssl/certificate.go`
- Create: `internal/ssl/certificate_test.go`
- Modify: `internal/ssl/service_test.go`

**Interfaces:**
- Produces: `certificateMetadata{NotBefore time.Time, NotAfter time.Time}`
- Produces: `validateCertificateMaterial(certPEM, keyPEM []byte, domain string, now time.Time) (certificateMetadata, error)`
- Produces: real ECDSA certificate fixtures for existing SSL service tests.

- [ ] **Step 1: Add a real certificate fixture helper**

In `certificate_test.go`, generate an ECDSA P-256 key and self-signed leaf whose
DNS names and validity are supplied by the test:

```go
func testCertificate(t *testing.T, dnsNames []string, notBefore, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: dnsNames, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil { t.Fatal(err) }
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
```

- [ ] **Step 2: Add failing table tests for material validation**

Cover valid exact domain, valid wildcard, malformed certificate, mismatched
key, wrong hostname, future `NotBefore`, expired `NotAfter`, certificate larger
than 256 KiB, and key larger than 64 KiB. Assert validation errors without
asserting raw crypto-library wording. The valid case must assert the literal
fixture `NotAfter` value.

- [ ] **Step 3: Run certificate tests and verify RED**

Run: `go test ./internal/ssl -run TestValidateCertificateMaterial -count=1`

Expected: build failure because `validateCertificateMaterial` does not exist.

- [ ] **Step 4: Implement validation**

Use these limits and validation order:

```go
const maxCertificatePEMSize = 256 << 10
const maxPrivateKeyPEMSize = 64 << 10

func validateCertificateMaterial(certPEM, keyPEM []byte, domain string, now time.Time) (certificateMetadata, error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 { return certificateMetadata{}, model.NewValidationError("certificate and private key are required") }
	if len(certPEM) > maxCertificatePEMSize || len(keyPEM) > maxPrivateKeyPEMSize { return certificateMetadata{}, model.NewValidationError("certificate material is too large") }
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil { return certificateMetadata{}, model.NewValidationError("certificate or private key is invalid or does not match") }
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil { return certificateMetadata{}, model.NewValidationError("certificate is invalid") }
	if err := leaf.VerifyHostname(domain); err != nil { return certificateMetadata{}, model.NewValidationError("certificate does not cover the selected domain") }
	if leaf.NotBefore.After(now) { return certificateMetadata{}, model.NewValidationError("certificate is not valid yet") }
	if !leaf.NotAfter.After(now) { return certificateMetadata{}, model.NewValidationError("certificate has expired") }
	return certificateMetadata{NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}, nil
}
```

- [ ] **Step 5: Replace fake PEM service fixtures**

Remove `fakeCertPEM` and `fakeKeyPEM`. Each ACME success test must return a real
fixture covering the requested domain and valid from one hour before the test
clock until a literal future timestamp. Keep failure tests returning no PEM.

- [ ] **Step 6: Run SSL tests and commit**

Run: `go test ./internal/ssl -count=1`

Expected: PASS.

```bash
git add internal/ssl/certificate.go internal/ssl/certificate_test.go internal/ssl/service_test.go
git commit -m "feat(ssl): validate certificate material"
```

---

### Task 3: Build Rollback-Safe Per-Domain Nginx Activation

**Files:**
- Create: `internal/ssl/nginx_config.go`
- Create: `internal/ssl/nginx_config_test.go`
- Modify: `internal/ssl/service.go`
- Modify: `internal/ssl/service_test.go`

**Interfaces:**
- Consumes: `nginx.IPv6Available()`, `website.RenderVhost`, and `website.RenderTLSVhost` from Task 1.
- Produces: `siteRecord` containing primary domain, aliases, document root, PHP version, app type, and log directory.
- Produces: `Service.activateCertificate(ctx, activationRequest) error`.
- Produces: `Service.syncHTTPConfig(ctx, siteRecord, []string) error`.
- Produces: rollback snapshots for certificate, HTTP config, HTTPS config, and enabled symlinks.

- [ ] **Step 1: Add failing site/domain-loading tests**

Insert a website with a primary domain plus alias and assert:

```go
site, err := svc.loadSiteForDomain(context.Background(), "ws-001", "www.example.com")
if err != nil { t.Fatal(err) }
if site.PrimaryDomain != "example.com" || site.Domain != "www.example.com" { t.Fatalf("unexpected site: %#v", site) }
```

Add a second assertion that `other.example.net` returns a domain validation
error even when the website ID exists.

- [ ] **Step 2: Run ownership tests and verify RED**

Run: `go test ./internal/ssl -run TestLoadSiteForDomain -count=1`

Expected: build failure because `loadSiteForDomain` does not exist.

- [ ] **Step 3: Implement registered-domain lookup**

Create an internal value with explicit fields:

```go
type siteRecord struct {
	WebsiteID, PrimaryDomain, Domain, DocumentRoot, PHPVersion, AppType, LogDir string
	Aliases []string
}

type activationRequest struct {
	Site            siteRecord
	CertificatePEM  []byte
	PrivateKeyPEM   []byte
	RedirectDomains []string
}
```

Query `websites` joined to `domains` with both `website_id=?` and `name=?` for
ownership, then query all aliases for rendering. Derive `LogDir` from the stored
web user (`/home/<web_user>/logs`), not from request text.

- [ ] **Step 4: Add failing activation and rollback tests**

Use a stateful test executor that performs `cp`, `install`, `ln`, `rm`, and
`test -e` against `t.TempDir()` paths and returns configurable results for
`nginx -t` and `systemctl reload nginx`. Assert observable state:

- successful activation writes cert/key, a per-domain HTTPS config, and an HTTP
  redirect while returning active status;
- the key mode is `0600`;
- `[::]` is absent when capability is false;
- failed `nginx -t` restores byte-identical old cert/key/config files;
- failed reload restores the old files and executes a second reload; and
- unique local temporary certificate/key files no longer exist after success or
  failure.

- [ ] **Step 5: Run activation tests and verify RED**

Run: `go test ./internal/ssl -run 'TestActivateCertificate|TestActivationRollback' -count=1`

Expected: build failure because `activationRequest` and `activateCertificate`
do not exist.

- [ ] **Step 6: Implement system-file helpers and rollback**

Use unique files and root-owned installation:

```go
func writeTempMaterial(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil { return "", err }
	name := f.Name()
	if err := f.Chmod(0600); err != nil { f.Close(); os.Remove(name); return "", err }
	if _, err := f.Write(data); err != nil { f.Close(); os.Remove(name); return "", err }
	if err := f.Close(); err != nil { os.Remove(name); return "", err }
	return name, nil
}
```

Create a sudo backup directory with
`mktemp -d /tmp/jenderal_ssl_backup_XXXXXX`. Snapshot each live path with `test -e` plus
`cp -a`. Install public certificates/configs with mode `0644`, private keys with
mode `0600`, and symlinks with `ln -sfn`. Every return path defers removal of
local temporary files and the sudo backup directory.

On validation/reload failure, restore every path that existed and remove every
new path that did not, run `nginx -t`, then reload the restored configuration.
Return the original activation error plus a rollback error suffix only if
restoration itself fails.

- [ ] **Step 7: Render and activate both virtual hosts**

`syncHTTPConfig` calls `website.RenderVhost` with every active SSL domain in
`RedirectDomains`. `activateCertificate` calls `website.RenderTLSVhost` for the
selected domain, installs the enabled symlink, updates the HTTP config, runs
`nginx -t`, and reloads. Pass `nginx.IPv6Available()` to both renderers.

- [ ] **Step 8: Run SSL/Nginx/website tests and commit**

Run: `go test ./internal/nginx ./internal/website ./internal/ssl -count=1`

Expected: PASS.

```bash
git add internal/ssl/nginx_config.go internal/ssl/nginx_config_test.go internal/ssl/service.go internal/ssl/service_test.go
git commit -m "feat(ssl): activate per-domain nginx TLS safely"
```

---

### Task 4: Complete Let's Encrypt, Custom, Renewal, Revoke, and SSL State

**Files:**
- Modify: `internal/ssl/service.go`
- Modify: `internal/ssl/service_test.go`
- Modify: `internal/ssl/renewal.go`

**Interfaces:**
- Produces: `Service.InstallCustom(ctx context.Context, websiteID, domain string, certPEM, keyPEM []byte) (model.SSLCertificate, error)`
- Produces: `Service.SetAutoRenew(ctx context.Context, certID string, enabled bool) error`
- Consumes: certificate validation from Task 2 and activation pipeline from Task 3.

- [ ] **Step 1: Add failing lifecycle tests**

Add focused tests that prove:

```go
cert, err := svc.InstallCustom(ctx, "ws-001", "www.example.com", certPEM, keyPEM)
if err != nil { t.Fatal(err) }
if cert.Issuer != "custom" || cert.AutoRenew { t.Fatalf("unexpected custom metadata: %#v", cert) }
if !cert.ExpiresAt.Equal(wantNotAfter) { t.Fatalf("expires_at = %v, want %v", cert.ExpiresAt, wantNotAfter) }
```

Also test: unregistered domains are rejected before ACME/custom file writes;
Let's Encrypt stores the leaf's actual `NotAfter`; custom replacement keeps one
current logical record and restores the old active record on activation failure;
`SetAutoRenew` rejects custom records; renewal rejects custom records; custom
revoke never calls `ACMEClient.RevokeCertificate`; and deleting one of two
active domains leaves `websites.ssl_enabled=1`.

- [ ] **Step 2: Run lifecycle tests and verify RED**

Run: `go test ./internal/ssl -run 'Test(InstallCustom|Issue_|SetAutoRenew|RenewCustom|RevokeCustom|DeleteKeepsWebsiteSSL)' -count=1`

Expected: build failures for missing methods, then assertion failures where the
old service assumes 90 days, calls ACME for custom records, or clears the
website flag unconditionally.

- [ ] **Step 3: Route Let's Encrypt through validation and activation**

Keep creation of a pending record before the ACME network call. After obtaining
PEM bytes, call `validateCertificateMaterial` and `activateCertificate`; only
then set `status=active`, actual `expires_at`, and `auto_renew=1`. On first
installation failure, store a sanitized failed status. Never concatenate PEM or
private-key content into the error.

- [ ] **Step 4: Implement custom installation/replacement**

Normalize the domain, load it through `loadSiteForDomain`, validate the pasted
material, and set `issuer=custom`, `auto_renew=0`, and parsed expiration. If a
record already exists for the website/domain, snapshot its database fields and
reuse its ID. Set it to `issuing` during replacement; restore the old active
fields if activation fails. First-time failures remain visible as `failed`.

- [ ] **Step 5: Make lifecycle operations issuer-aware**

Implement:

```go
func (s *Service) SetAutoRenew(ctx context.Context, certID string, enabled bool) error {
	cert, err := s.Get(ctx, certID)
	if err != nil { return err }
	if cert.Issuer != "letsencrypt" { return model.NewValidationError("auto-renew is only available for Let's Encrypt certificates") }
	_, err = s.db.ExecContext(ctx, `UPDATE ssl_certificates SET auto_renew = ?, updated_at = ? WHERE id = ?`, boolToInt(enabled), time.Now().UTC().Format(time.RFC3339), certID)
	return err
}
```

Renew only `letsencrypt`; revoke through ACME only when issuer is
`letsencrypt`. After revoke/delete, query `COUNT(*) WHERE website_id=? AND
status='active'` and set `websites.ssl_enabled` from that count. Re-render the
HTTP config with the remaining active domains.

- [ ] **Step 6: Keep the renewal worker scoped to Let's Encrypt**

Add `AND issuer = 'letsencrypt'` to the expiring-certificate query. Test with an
active custom certificate whose `auto_renew` was manually set to `1` in a
fixture and assert it is still excluded.

- [ ] **Step 7: Run SSL tests and commit**

Run: `go test ./internal/ssl -count=1`

Expected: PASS.

```bash
git add internal/ssl/service.go internal/ssl/service_test.go internal/ssl/renewal.go
git commit -m "feat(ssl): add secure custom certificate lifecycle"
```

---

### Task 5: Expose and Verify the SSL API Contract

**Files:**
- Modify: `internal/ssl/handler.go`
- Create: `internal/ssl/handler_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Produces: `POST /api/v1/ssl/custom`
- Produces: `PUT /api/v1/ssl/{id}` with `{ "auto_renew": boolean }`
- Retains: `POST /api/v1/ssl/issue` and existing lifecycle routes.

- [ ] **Step 1: Add failing handler tests**

Add tests with `httptest` for request decoding and response safety. Use a real
test service/database plus the stateful test executor from Task 3. Cover:

- valid custom JSON returns the certificate without PEM/key fields;
- body over `512 << 10` bytes returns HTTP 400;
- malformed/empty PEM returns HTTP 400;
- `PUT` on a Let's Encrypt record updates auto-renew;
- `PUT` on a custom record returns HTTP 400; and
- audit details contain the domain and issuer but not certificate/key content.

- [ ] **Step 2: Run handler tests and verify RED**

Run: `go test ./internal/ssl -run TestHandler -count=1`

Expected: build failure because custom and update handlers do not exist.

- [ ] **Step 3: Add request types and handlers**

Add exact request shapes:

```go
type customRequest struct {
	WebsiteID     string `json:"website_id"`
	Domain        string `json:"domain"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

type autoRenewRequest struct {
	AutoRenew bool `json:"auto_renew"`
}
```

In `InstallCustom`, wrap the request body with
`http.MaxBytesReader(w, r.Body, 512<<10)` before `DecodeJSON`. Call the service
with byte slices, log only certificate ID/domain/issuer, clear local string
references after the call, and return the normal certificate response. In
`Update`, call `SetAutoRenew` and return `{ "status": "ok" }`.

- [ ] **Step 4: Register protected routes**

Under `ssl.manage`, add:

```go
r.Post("/ssl/custom", sslHandler.InstallCustom)
r.Put("/ssl/{id}", sslHandler.Update)
```

Keep `/ssl/issue`, renew, revoke, and delete unchanged.

- [ ] **Step 5: Run API-adjacent tests and commit**

Run: `go test ./internal/ssl ./internal/api -count=1`

Expected: PASS.

```bash
git add internal/ssl/handler.go internal/ssl/handler_test.go internal/api/router.go
git commit -m "feat(ssl): expose custom install and auto-renew API"
```

---

### Task 6: Build the Per-Domain SSL Management UI

**Files:**
- Create: `web/src/lib/ssl-form.js`
- Create: `web/tests/ssl/SSLForm.test.mjs`
- Modify: `web/package.json`
- Modify: `web/src/routes/ssl/+page.svelte`
- Modify: `web/src/routes/websites/[id]/+page.svelte`

**Interfaces:**
- Produces: `domainsForWebsite(websites, websiteID)`
- Produces: `buildSSLInstallRequest(mode, values)` returning `{ path, body }`
- Consumes: `/api/v1/websites`, `/api/v1/ssl/issue`, `/api/v1/ssl/custom`, and certificate issuer/status fields.

- [ ] **Step 1: Add failing frontend behavior tests**

Create `SSLForm.test.mjs`:

```js
import { test } from 'node:test';
import assert from 'node:assert/strict';

let domainsForWebsite;
let buildSSLInstallRequest;
try {
	({ domainsForWebsite, buildSSLInstallRequest } = await import('../../src/lib/ssl-form.js'));
} catch {}

test('offers only domains registered to the selected website', () => {
	assert.equal(typeof domainsForWebsite, 'function');
	const websites = [{ id: 'ws-1', domain: 'example.com', domains: [{ name: 'example.com' }, { name: 'www.example.com' }] }, { id: 'ws-2', domain: 'other.test', domains: [{ name: 'other.test' }] }];
	assert.deepEqual(domainsForWebsite(websites, 'ws-1'), ['example.com', 'www.example.com']);
});

test('uses the existing lets encrypt issue endpoint', () => {
	assert.deepEqual(buildSSLInstallRequest('letsencrypt', { websiteId: 'ws-1', domain: 'example.com', certificatePEM: '', privateKeyPEM: '' }), { path: '/api/v1/ssl/issue', body: { website_id: 'ws-1', domain: 'example.com' } });
});

test('sends pasted material only to the custom endpoint', () => {
	assert.deepEqual(buildSSLInstallRequest('custom', { websiteId: 'ws-1', domain: 'example.com', certificatePEM: 'CERT', privateKeyPEM: 'KEY' }), { path: '/api/v1/ssl/custom', body: { website_id: 'ws-1', domain: 'example.com', certificate_pem: 'CERT', private_key_pem: 'KEY' } });
});
```

Add `tests/ssl/*.test.mjs` to the `npm test` command.

- [ ] **Step 2: Run frontend tests and verify RED**

Run: `cd web && npm test -- --test-name-pattern='registered|issue endpoint|custom endpoint'`

Expected: failures because the helper exports do not exist.

- [ ] **Step 3: Implement pure form helpers**

Create `ssl-form.js` with exact-domain deduplication and request construction:

```js
export function domainsForWebsite(websites, websiteID) {
	const website = websites.find((item) => item.id === websiteID);
	if (!website) return [];
	return [...new Set([website.domain, ...(website.domains || []).map((domain) => domain.name)].filter(Boolean))];
}

export function buildSSLInstallRequest(mode, values) {
	const base = { website_id: values.websiteId, domain: values.domain.trim() };
	if (mode === 'custom') return { path: '/api/v1/ssl/custom', body: { ...base, certificate_pem: values.certificatePEM, private_key_pem: values.privateKeyPEM } };
	return { path: '/api/v1/ssl/issue', body: base };
}
```

- [ ] **Step 4: Replace the free-domain form with registered selection and method toggle**

Extend the Website interface with `domains: { name: string; type: string }[]`.
Replace the editable domain input with a select populated by
`domainsForWebsite`. Add `installMode: 'letsencrypt' | 'custom'`, accessible
toggle buttons, and conditionally rendered Certificate / Full Chain PEM and
Private Key PEM textareas. Disable Install until all mode-specific fields are
present.

`issueCertificate` must call `buildSSLInstallRequest`, post to its path, and
clear both PEM variables in `finally` when the form closes or after success.
Never use local/session storage for these fields.

- [ ] **Step 5: Make certificate actions issuer-aware**

Show Auto Renew and Renew only when `cert.issuer === 'letsencrypt'`. For custom
records show Replace, opening the form preselected to that certificate's
website/domain and custom mode. Keep delete for both issuers; show revoke only
for Let's Encrypt. Wire the existing toggle to `PUT /api/v1/ssl/{id}`.

- [ ] **Step 6: Add website-detail SSL status link**

Extend the detail Website interface with `ssl_enabled: boolean`. Render an SSL
status row and a link to `/ssl` labeled `Manage SSL Certificates`; do not add a
second install form.

- [ ] **Step 7: Run frontend checks and commit**

Run: `cd web && npm test && npm run check && npm run build`

Expected: all tests and build pass; existing unrelated Svelte warnings may
remain but no new warning may originate from SSL files.

```bash
git add web/src/lib/ssl-form.js web/tests/ssl/SSLForm.test.mjs web/package.json web/src/routes/ssl/+page.svelte 'web/src/routes/websites/[id]/+page.svelte'
git commit -m "feat(ssl): add per-domain certificate management UI"
```

---

### Task 7: End-to-End Regression Gate, Review, and Push

**Files:**
- Verify all files committed by Tasks 1-6.
- Modify only files required to resolve Critical or Important review findings.

**Interfaces:**
- Validates the complete spec and preserves the `main` integration target requested by the user.

- [ ] **Step 1: Reproduce the original IPv4-only outcome in tests**

Run: `go test ./internal/nginx ./internal/website ./internal/ssl -run 'IPv6|Activate|InstallCustom|Issue|RenderVhost|RenderTLSVhost' -count=1`

Expected: PASS, including assertions that IPv4-only output contains no `[::]`
listener and retry rendering uses the detected value.

- [ ] **Step 2: Run the complete backend gate**

Run: `go test ./... && go vet ./...`

Expected: PASS with zero failures.

- [ ] **Step 3: Run the complete frontend gate**

Run: `cd web && npm test && npm run check && npm run build`

Expected: tests, Svelte check, and production build exit 0. Record any existing
unrelated warning separately.

- [ ] **Step 4: Check the final diff**

Run: `git status --short --branch && git diff --check && git log --oneline -8`

Expected: no uncommitted production/test changes, no whitespace errors, and the
task commits appear after the design/plan commits.

- [ ] **Step 5: Request a read-only code review**

Give the reviewer the spec path, plan path, base commit `48c5159`, final HEAD,
and ask specifically about private-key exposure, path injection, Nginx rollback,
IPv6 detection, multi-domain SSL state, ACME renewal, and API/UI compatibility.
Resolve every Critical and Important finding, then rerun Steps 1-4.

- [ ] **Step 6: Push the verified main branch**

```bash
git push origin main
git fetch origin main
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
git status --short --branch
```

Expected: push succeeds, local HEAD equals `origin/main`, and the worktree is
clean.

- [ ] **Step 7: Hand off VPS recovery steps**

Report the final commit SHA and these exact operator actions:

1. Run **Update Now** in Jenderal Panel.
2. Retry the failed website; its Nginx config is regenerated without IPv6 on
   this VPS.
3. Confirm `sudo nginx -t` succeeds.
4. Open **SSL Certificates**, choose the website and registered domain, choose
   Let's Encrypt or Custom SSL, then install.
5. Confirm `curl -I http://<domain>` returns an HTTPS redirect and
   `curl -I https://<domain>` returns the application response.
