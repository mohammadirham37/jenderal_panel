package website

import (
	"bytes"
	"strings"
	"text/template"
)

// VhostData holds variables for rendering an Nginx virtual host configuration.
type VhostData struct {
	Domain       string
	Aliases      string
	DocumentRoot string
	LogDir       string
	PHPVersion   string
	AppType      string
}

// PoolData holds variables for rendering a PHP-FPM pool configuration.
type PoolData struct {
	Domain     string
	WebUser    string
	PHPVersion string
	HomeDir    string
	LogDir     string
}

const vhostPHPTemplate = `server {
    listen 80;
    listen [::]:80;

    server_name {{ .Domain }}{{ if .Aliases }} {{ .Aliases }}{{ end }};

    root {{ .DocumentRoot }};
    index index.php index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

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

const vhostStaticTemplate = `server {
    listen 80;
    listen [::]:80;

    server_name {{ .Domain }}{{ if .Aliases }} {{ .Aliases }}{{ end }};

    root {{ .DocumentRoot }};
    index index.html index.htm;

    access_log {{ .LogDir }}/access.log;
    error_log {{ .LogDir }}/error.log;

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
