package website

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// nginxVarSanitizer strips characters nginx does not accept in variable names.
var nginxVarSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// websocketMapSuffix builds a per-domain, per-scheme unique map variable
// suffix so multiple Octane sites (and the HTTP + HTTPS vhost pair of one
// site) never redefine the same map.
func websocketMapSuffix(domain string, tls bool) string {
	suffix := nginxVarSanitizer.ReplaceAllString(domain, "_")
	if tls {
		return suffix + "_wss"
	}
	return suffix + "_ws"
}

// VhostData holds variables for rendering an Nginx virtual host configuration.
type VhostData struct {
	Domain            string
	Aliases           string
	DocumentRoot      string
	ACMEChallengeRoot string
	LogDir            string
	PHPVersion        string
	AppType           string
	Profile           string
	IPv6              bool
	RedirectDomains   []string
	SecurityInclude   string
	// OctanePort is the loopback port of the site's Octane worker; it is
	// required by the laravel-octane proxy profile.
	OctanePort int
	// AppPort is the loopback port of the site's app service (node/go/...);
	// required by the app-proxy profile.
	AppPort int
}

const DefaultACMEChallengeRoot = "/var/lib/jenderal/acme-challenges"

// TLSVhostData holds the website and certificate data for one HTTPS server.
type TLSVhostData struct {
	VhostData
	TLSDomain       string
	CertificatePath string
	PrivateKeyPath  string
}

// PoolData holds variables for rendering a PHP-FPM pool configuration.
type PoolData struct {
	Domain     string
	WebUser    string
	PHPVersion string
	HomeDir    string
	LogDir     string
}

const vhostPHPTemplate = `{{ if .ApplicationDomains }}server {
    listen 80;
    {{ if .IPv6 }}listen [::]:80;{{ end }}

    server_name {{ .ApplicationDomains }};

    root {{ .DocumentRoot }};
    index index.php index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

    location ^~ /.well-known/acme-challenge/ {
        root {{ .ACMEChallengeRoot }};
        try_files $uri =404;
    }

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php{{ .PHPVersion }}-fpm-{{ .Domain }}.sock;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }

    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
{{ end }}{{ range .RedirectDomains }}
server {
    listen 80;
    {{ if $.IPv6 }}listen [::]:80;{{ end }}

    server_name {{ . }};
    root {{ $.DocumentRoot }};

    location ^~ /.well-known/acme-challenge/ {
        root {{ $.ACMEChallengeRoot }};
        try_files $uri =404;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}
{{ end }}`

const vhostStaticTemplate = `{{ if .ApplicationDomains }}server {
    listen 80;
    {{ if .IPv6 }}listen [::]:80;{{ end }}

    server_name {{ .ApplicationDomains }};

    root {{ .DocumentRoot }};
    index index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

    location ^~ /.well-known/acme-challenge/ {
        root {{ .ACMEChallengeRoot }};
        try_files $uri =404;
    }

    location / {
        try_files $uri $uri/ =404;
    }

    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
{{ end }}{{ range .RedirectDomains }}
server {
    listen 80;
    {{ if $.IPv6 }}listen [::]:80;{{ end }}

    server_name {{ . }};
    root {{ $.DocumentRoot }};

    location ^~ /.well-known/acme-challenge/ {
        root {{ $.ACMEChallengeRoot }};
        try_files $uri =404;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}
{{ end }}`

const tlsVhostPHPTemplate = `server {
    listen 443 ssl;
    {{ if .IPv6 }}listen [::]:443 ssl;{{ end }}

    server_name {{ .TLSDomain }};

    root {{ .DocumentRoot }};
    index index.php index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

    ssl_certificate {{ .CertificatePath }};
    ssl_certificate_key {{ .PrivateKeyPath }};
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php{{ .PHPVersion }}-fpm-{{ .Domain }}.sock;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }

    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
`

const tlsVhostStaticTemplate = `server {
    listen 443 ssl;
    {{ if .IPv6 }}listen [::]:443 ssl;{{ end }}

    server_name {{ .TLSDomain }};

    root {{ .DocumentRoot }};
    index index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

    ssl_certificate {{ .CertificatePath }};
    ssl_certificate_key {{ .PrivateKeyPath }};
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        try_files $uri $uri/ =404;
    }

    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }
}
`

const profileHTTPTemplate = `{{ .Directives.Header }}{{ if .ApplicationDomains }}server {
    listen 80;
    {{ if .IPv6 }}listen [::]:80;{{ end }}

    server_name {{ .ApplicationDomains }};
    root {{ .DocumentRoot }};
    index {{ .Directives.Index }};

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;
    {{ if .SecurityInclude }}include {{ .SecurityInclude }};{{ end }}
{{ .Directives.Server }}
    location ^~ /.well-known/acme-challenge/ {
        root {{ .ACMEChallengeRoot }};
        try_files $uri =404;
    }

{{ .Directives.Location }}
{{ .Directives.PHP }}
{{ .Directives.Hidden }}
}
{{ end }}{{ range .RedirectDomains }}
server {
    listen 80;
    {{ if $.IPv6 }}listen [::]:80;{{ end }}

    server_name {{ . }};
    root {{ $.DocumentRoot }};

    location ^~ /.well-known/acme-challenge/ {
        root {{ $.ACMEChallengeRoot }};
        try_files $uri =404;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}
{{ end }}`

const profileTLSTemplate = `{{ .Directives.Header }}server {
    listen 443 ssl;
    {{ if .IPv6 }}listen [::]:443 ssl;{{ end }}

    server_name {{ .TLSDomain }};
    root {{ .DocumentRoot }};
    index {{ .Directives.Index }};

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;
    {{ if .SecurityInclude }}include {{ .SecurityInclude }};{{ end }}

    ssl_certificate {{ .CertificatePath }};
    ssl_certificate_key {{ .PrivateKeyPath }};
    ssl_protocols TLSv1.2 TLSv1.3;
{{ .Directives.Server }}
{{ .Directives.Location }}
{{ .Directives.PHP }}
{{ .Directives.Hidden }}
}
`

type nginxProfileDirectives struct {
	// Header is rendered at the top of the vhost file, outside the server
	// block. The laravel-octane profile uses it for the websocket upgrade
	// map, which nginx only accepts in the http context.
	Header   string
	Index    string
	Server   string
	Location string
	PHP      string
	Hidden   string
}

func directivesFor(data VhostData) nginxProfileDirectives {
	return directivesForProfile(data, false)
}

// directivesForProfile renders the per-profile directives. tls only affects
// the websocket map variable name: the HTTP and HTTPS vhosts are separate
// files in the same http context, so their maps must not collide.
func directivesForProfile(data VhostData, tls bool) nginxProfileDirectives {
	profile := data.Profile
	if profile == "" {
		if data.AppType == "static" {
			profile = "static"
		} else if data.AppType == "laravel" {
			profile = "laravel"
		} else {
			profile = "php"
		}
	}
	standardHidden := `    location ~ /\. {
        deny all;
        access_log off;
        log_not_found off;
    }`
	standardPHP := `    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php` + data.PHPVersion + `-fpm-` + data.Domain + `.sock;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }`
	switch profile {
	case "static":
		return nginxProfileDirectives{Index: "index.html index.htm", Location: `    location / {
        try_files $uri $uri/ =404;
    }`, Hidden: standardHidden}
	case "wordpress":
		// WordPress: permalink-friendly front controller plus hardening for
		// the SQLite database directory (the auto-install keeps the database
		// file under wp-content/database, which nginx must never serve) and
		// for xmlrpc.php, which is almost exclusively abuse traffic.
		return nginxProfileDirectives{Index: "index.php", Server: `    # Block direct access to the SQLite database directory (auto-install).
    location ^~ /wp-content/database/ {
        deny all;
        access_log off;
        log_not_found off;
    }

    # xmlrpc.php is almost exclusively brute-force traffic; delete this
    # block if a plugin or client genuinely needs it.
    location = /xmlrpc.php {
        deny all;
        access_log off;
    }
`, Location: `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }`, PHP: standardPHP, Hidden: standardHidden}
	case "app-proxy":
		if data.AppPort <= 0 {
			return nginxProfileDirectives{Index: "index.html index.htm", Hidden: standardHidden}
		}
		suffix := nginxVarSanitizer.ReplaceAllString(data.Domain, "_")
		mapVar := "$jenderal_app_" + suffix
		upstream := "http://127.0.0.1:" + strconv.Itoa(data.AppPort)
		location := "    location / {\n        try_files $uri $uri/ @app;\n    }\n\n    location @app {\n        proxy_pass " + upstream + ";\n        proxy_http_version 1.1;\n        proxy_set_header Host $host;\n        proxy_set_header X-Real-IP $remote_addr;\n        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n        proxy_set_header X-Forwarded-Proto $scheme;\n        proxy_set_header Upgrade $http_upgrade;\n        proxy_set_header Connection " + mapVar + ";\n        proxy_read_timeout 300s;\n        proxy_send_timeout 300s;\n    }"
		return nginxProfileDirectives{Index: "index.html index.htm", Header: "map $http_upgrade " + mapVar + ` {
    default upgrade;
    ''      close;
}
`, Location: location, Hidden: standardHidden}
	case "codeigniter3":
		return nginxProfileDirectives{Index: "index.php index.html index.htm", Server: "    error_page 404 /index.php;\n", Location: `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }`, PHP: standardPHP, Hidden: standardHidden}
	case "codeigniter4":
		return nginxProfileDirectives{Index: "index.php index.html index.htm", Location: `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }`, PHP: standardPHP, Hidden: standardHidden}
	case "laravel":
		return nginxProfileDirectives{Index: "index.php", Server: `    add_header X-Frame-Options SAMEORIGIN always;
    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy strict-origin-when-cross-origin always;
`, Location: `    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }`, PHP: `    location ~ ^/index\.php(/|$) {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php` + data.PHPVersion + `-fpm-` + data.Domain + `.sock;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        fastcgi_param DOCUMENT_ROOT $realpath_root;
        internal;
    }`, Hidden: `    location ~ /\.(?!well-known).* {
        deny all;
        access_log off;
        log_not_found off;
    }`}
	case "laravel-octane":
		if data.OctanePort <= 0 {
			return nginxProfileDirectives{Index: "index.php", Hidden: standardHidden}
		}
		suffix := websocketMapSuffix(data.Domain, tls)
		mapVar := "$jenderal_ws_" + suffix
		header := "map $http_upgrade " + mapVar + ` {
    default upgrade;
    ''      close;
}
`
		location := `    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    location / {
        try_files $uri $uri/ @octane;
    }

    location @octane {
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header Scheme $scheme;
        proxy_set_header SERVER_PORT $server_port;
        proxy_set_header REMOTE_ADDR $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection ` + mapVar + `;
        proxy_pass http://127.0.0.1:` + strconv.Itoa(data.OctanePort) + `;
    }`
		return nginxProfileDirectives{Index: "index.php", Header: header, Location: location,
			Hidden: `    location ~ /\.(?!well-known).* {
        deny all;
        access_log off;
        log_not_found off;
    }`}
	default:
		return nginxProfileDirectives{Index: "index.php index.html index.htm", Location: `    location / {
		try_files $uri $uri/ /index.php?$query_string;
	}`, PHP: standardPHP, Hidden: standardHidden}
	}
}

const poolTemplate = `[{{ .Domain }}]
user = {{ .WebUser }}
group = {{ .WebUser }}

listen = /run/php/php{{ .PHPVersion }}-fpm-{{ .Domain }}.sock
listen.owner = {{ .WebUser }}
listen.group = www-data
listen.mode = 0660

pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3

php_admin_value[open_basedir] = {{ .HomeDir }}:/tmp:/usr/share/php
php_admin_value[session.save_path] = {{ .HomeDir }}/tmp
php_admin_value[error_log] = {{ .LogDir }}/php-error.log

slowlog = {{ .LogDir }}/php-slow.log
request_slowlog_timeout = 10s
`

// RenderVhost renders an Nginx virtual host configuration using the given data.
// The AppType field selects between a PHP-enabled template and a static template.
func RenderVhost(data VhostData) (string, error) {
	tmpl, err := template.New("vhost").Parse(profileHTTPTemplate)
	if err != nil {
		return "", err
	}

	renderData := prepareHTTPVhostData(data)
	renderData.Directives = directivesFor(data)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, renderData); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type httpVhostData struct {
	VhostData
	ApplicationDomains string
	Directives         nginxProfileDirectives
}

func prepareHTTPVhostData(data VhostData) httpVhostData {
	if data.ACMEChallengeRoot == "" {
		data.ACMEChallengeRoot = data.DocumentRoot
	}
	allDomains := uniqueDomains(append([]string{data.Domain}, strings.Fields(data.Aliases)...))
	known := make(map[string]struct{}, len(allDomains))
	for _, domain := range allDomains {
		known[domain] = struct{}{}
	}

	redirectSet := make(map[string]struct{}, len(data.RedirectDomains))
	redirectDomains := make([]string, 0, len(data.RedirectDomains))
	for _, domain := range data.RedirectDomains {
		if _, ok := known[domain]; !ok {
			continue
		}
		if _, exists := redirectSet[domain]; exists {
			continue
		}
		redirectSet[domain] = struct{}{}
		redirectDomains = append(redirectDomains, domain)
	}

	applicationDomains := make([]string, 0, len(allDomains))
	for _, domain := range allDomains {
		if _, redirected := redirectSet[domain]; !redirected {
			applicationDomains = append(applicationDomains, domain)
		}
	}

	data.RedirectDomains = redirectDomains
	return httpVhostData{
		VhostData:          data,
		ApplicationDomains: strings.Join(applicationDomains, " "),
	}
}

func uniqueDomains(domains []string) []string {
	seen := make(map[string]struct{}, len(domains))
	result := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		result = append(result, domain)
	}
	return result
}

// RenderTLSVhost renders one HTTPS application server for a registered domain.
func RenderTLSVhost(data TLSVhostData) (string, error) {
	tmpl, err := template.New("tls-vhost").Parse(profileTLSTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	renderData := struct {
		TLSVhostData
		Directives nginxProfileDirectives
	}{data, directivesForProfile(data.VhostData, true)}
	if err := tmpl.Execute(&buf, renderData); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderPool renders a PHP-FPM pool configuration using the given data.
func RenderPool(data PoolData) (string, error) {
	tmpl, err := template.New("pool").Parse(poolTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// DomainToUser converts a domain name to a system user name.
// Dots and hyphens are replaced with underscores, and the result is prefixed
// with "web_". For example, "example.com" becomes "web_example_com".
func DomainToUser(domain string) string {
	s := strings.ReplaceAll(domain, ".", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return "web_" + s
}
