package website

import (
	"bytes"
	"strings"
	"text/template"
)

// VhostData holds variables for rendering an Nginx virtual host configuration.
type VhostData struct {
	Domain            string
	Aliases           string
	DocumentRoot      string
	ACMEChallengeRoot string
	LogDir            string
	PHPVersion        string
	AppType           string
	IPv6              bool
	RedirectDomains   []string
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
	tmplStr := vhostPHPTemplate
	if data.AppType == "static" {
		tmplStr = vhostStaticTemplate
	}

	tmpl, err := template.New("vhost").Parse(tmplStr)
	if err != nil {
		return "", err
	}

	renderData := prepareHTTPVhostData(data)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, renderData); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type httpVhostData struct {
	VhostData
	ApplicationDomains string
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
	tmplStr := tlsVhostPHPTemplate
	if data.AppType == "static" {
		tmplStr = tlsVhostStaticTemplate
	}

	tmpl, err := template.New("tls-vhost").Parse(tmplStr)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
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
