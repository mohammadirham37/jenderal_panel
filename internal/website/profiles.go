package website

import (
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	SetupConfigOnly = "config-only"
	SetupAutomatic  = "automatic"
)

// Profile is the server-derived deployment contract for an allowlisted website template.
type Profile struct {
	Template             string `json:"template"`
	AppType              string `json:"app_type"`
	NginxProfile         string `json:"nginx_profile"`
	Framework            string `json:"framework"`
	FrameworkVersion     string `json:"framework_version"`
	FrontendStack        string `json:"frontend_stack"`
	InertiaAdapter       string `json:"inertia_adapter"`
	ProjectVariant       string `json:"project_variant"`
	SetupMode            string `json:"setup_mode"`
	MinimumPHP           string `json:"minimum_php"`
	RelativeProjectRoot  string `json:"relative_project_root"`
	RelativeDocumentRoot string `json:"relative_document_root"`
	RequiresComposer     bool   `json:"requires_composer"`
	RequiresNode         bool   `json:"requires_node"`
	StarterRepository    string `json:"-"`
	StarterReference     string `json:"-"`
	StarterCommit        string `json:"-"`
}

var laravelMinimumPHP = map[string]string{
	"8": "8.1", "9": "8.1", "10": "8.1", "11": "8.2", "12": "8.2", "13": "8.3",
}

type starterSource struct {
	repository string
	reference  string
	commit     string
}

var laravelStarterSources = map[string]map[string]starterSource{
	"12": {
		"react":    {repository: "laravel/react-starter-kit", reference: "v1.0.1"},
		"vue":      {repository: "laravel/vue-starter-kit", reference: "v1.0.2"},
		"livewire": {repository: "laravel/livewire-starter-kit", reference: "v1.0.1"},
	},
	"13": {
		"react":    {repository: "laravel/react-starter-kit", commit: "87cce8705d712629ebddd70ccfbb06592ecbaac2"},
		"vue":      {repository: "laravel/vue-starter-kit", commit: "12c8185609b10802b96d740dc5c4fcac0d66ade3"},
		"svelte":   {repository: "laravel/svelte-starter-kit", commit: "593365653c38308fbea55fc90dfb22c554702b81"},
		"livewire": {repository: "laravel/livewire-starter-kit", commit: "78fed019a1848eb42ff15911ef3c5a22042755fc"},
	},
}

// ResolveProfile validates request selections and derives paths and runtime requirements.
// Package names and source revisions come only from this file's allowlists.
func ResolveProfile(req CreateRequest) (Profile, error) {
	template := strings.TrimSpace(req.Template)
	if template == "" {
		template = strings.TrimSpace(req.AppType)
	}
	if template == "" {
		template = "php"
	}
	setupMode := valueOr(req.SetupMode, SetupConfigOnly)
	if setupMode != SetupConfigOnly && setupMode != SetupAutomatic {
		return Profile{}, model.NewValidationError("setup_mode must be config-only or automatic")
	}

	switch template {
	case "static":
		return simpleProfile(req, template, "static", "none", "", "public", setupMode, false)
	case "php":
		return simpleProfile(req, template, "php", "none", "", "public", setupMode, false)
	case "codeigniter3":
		return simpleProfile(req, template, "php", "codeigniter", "3.1.13", "public", setupMode, false)
	case "codeigniter4":
		return simpleProfile(req, template, "php", "codeigniter", "4", "app/public", setupMode, true)
	case "laravel":
		return resolveLaravelProfile(req, setupMode)
	default:
		return Profile{}, model.NewValidationError("unsupported website template: " + template)
	}
}

func simpleProfile(req CreateRequest, template, appType, framework, frameworkVersion, documentRoot, setupMode string, composer bool) (Profile, error) {
	if template == "static" {
		if req.PHPVersion != "" {
			return Profile{}, model.NewValidationError("static websites do not use PHP")
		}
	} else if !supportedPHP(req.PHPVersion) {
		return Profile{}, model.NewValidationError("unsupported PHP version: " + req.PHPVersion)
	}
	if req.FrameworkVersion != "" && req.FrameworkVersion != frameworkVersion {
		return Profile{}, model.NewValidationError("unsupported framework version for " + template)
	}
	if req.FrontendStack != "" || req.InertiaAdapter != "" || (req.ProjectVariant != "" && req.ProjectVariant != "empty") {
		return Profile{}, model.NewValidationError(template + " does not support Laravel frontend selections")
	}
	projectRoot := ""
	if documentRoot == "app/public" {
		projectRoot = "app"
	}
	return Profile{
		Template: template, AppType: appType, NginxProfile: template,
		Framework: framework, FrameworkVersion: frameworkVersion,
		ProjectVariant: "empty", SetupMode: setupMode,
		RelativeProjectRoot: projectRoot, RelativeDocumentRoot: documentRoot,
		RequiresComposer: composer && setupMode == SetupAutomatic,
	}, nil
}

func resolveLaravelProfile(req CreateRequest, setupMode string) (Profile, error) {
	version := valueOr(req.FrameworkVersion, "12")
	minimumPHP, ok := laravelMinimumPHP[version]
	if !ok {
		return Profile{}, model.NewValidationError("unsupported Laravel version: " + version)
	}
	if !supportedPHP(req.PHPVersion) {
		return Profile{}, model.NewValidationError("unsupported PHP version: " + req.PHPVersion)
	}
	if comparePHP(req.PHPVersion, minimumPHP) < 0 {
		return Profile{}, model.NewValidationError(fmt.Sprintf("Laravel %s requires PHP %s or newer", version, minimumPHP))
	}

	frontend := valueOr(req.FrontendStack, "blade")
	variant := valueOr(req.ProjectVariant, "empty")
	if variant != "empty" && variant != "starter-kit" {
		return Profile{}, model.NewValidationError("project_variant must be empty or starter-kit")
	}
	adapter := strings.TrimSpace(req.InertiaAdapter)
	key := frontend
	switch frontend {
	case "blade":
		if adapter != "" {
			return Profile{}, model.NewValidationError("Blade does not use an Inertia adapter")
		}
	case "inertia":
		if adapter != "react" && adapter != "vue" && adapter != "svelte" {
			return Profile{}, model.NewValidationError("Inertia adapter must be react, vue, or svelte")
		}
		key = adapter
	case "livewire":
		if adapter != "" {
			return Profile{}, model.NewValidationError("Livewire does not use an Inertia adapter")
		}
	default:
		return Profile{}, model.NewValidationError("frontend_stack must be blade, inertia, or livewire")
	}

	profile := Profile{
		Template: "laravel", AppType: "laravel", NginxProfile: "laravel",
		Framework: "laravel", FrameworkVersion: version, FrontendStack: frontend,
		InertiaAdapter: adapter, ProjectVariant: variant, SetupMode: setupMode,
		MinimumPHP: minimumPHP, RelativeProjectRoot: "app", RelativeDocumentRoot: "app/public",
		RequiresComposer: setupMode == SetupAutomatic,
	}
	if setupMode != SetupAutomatic {
		return profile, nil
	}
	if variant == "empty" {
		if frontend != "blade" {
			return Profile{}, model.NewValidationError("automatic installation for empty Inertia or Livewire projects is not available; use config-only")
		}
		return profile, nil
	}
	if frontend == "blade" {
		return Profile{}, model.NewValidationError("automatic Blade starter kit is not available; choose an empty project")
	}
	source, available := laravelStarterSources[version][key]
	if !available {
		return Profile{}, model.NewValidationError(fmt.Sprintf("automatic %s starter kit is not available for Laravel %s; use config-only", key, version))
	}
	profile.RequiresNode = true
	profile.StarterRepository = source.repository
	profile.StarterReference = source.reference
	profile.StarterCommit = source.commit
	return profile, nil
}

func supportedPHP(version string) bool {
	return version == "8.1" || version == "8.2" || version == "8.3" || version == "8.4"
}

func comparePHP(left, right string) int {
	return strings.Compare(left, right)
}

// NginxProfileFor derives the renderer profile for persisted and legacy rows.
func NginxProfileFor(framework, frameworkVersion, appType string) string {
	switch framework {
	case "laravel":
		return "laravel"
	case "codeigniter":
		if strings.HasPrefix(frameworkVersion, "3") {
			return "codeigniter3"
		}
		return "codeigniter4"
	}
	if appType == "static" {
		return "static"
	}
	if appType == "laravel" {
		return "laravel"
	}
	return "php"
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func websiteProfileOptions() []ProfileOption {
	var requests []CreateRequest
	for _, template := range []string{"static", "php", "codeigniter3", "codeigniter4"} {
		phpVersion := "8.1"
		if template == "static" {
			phpVersion = ""
		}
		for _, mode := range []string{SetupConfigOnly, SetupAutomatic} {
			requests = append(requests, CreateRequest{Template: template, PHPVersion: phpVersion, SetupMode: mode})
		}
	}
	for _, version := range []string{"8", "9", "10", "11", "12", "13"} {
		phpVersion := laravelMinimumPHP[version]
		for _, mode := range []string{SetupConfigOnly, SetupAutomatic} {
			requests = append(requests, CreateRequest{Template: "laravel", FrameworkVersion: version, PHPVersion: phpVersion, FrontendStack: "blade", ProjectVariant: "empty", SetupMode: mode})
			for _, adapter := range []string{"react", "vue", "svelte"} {
				for _, variant := range []string{"empty", "starter-kit"} {
					requests = append(requests, CreateRequest{Template: "laravel", FrameworkVersion: version, PHPVersion: phpVersion, FrontendStack: "inertia", InertiaAdapter: adapter, ProjectVariant: variant, SetupMode: mode})
				}
			}
			for _, variant := range []string{"empty", "starter-kit"} {
				requests = append(requests, CreateRequest{Template: "laravel", FrameworkVersion: version, PHPVersion: phpVersion, FrontendStack: "livewire", ProjectVariant: variant, SetupMode: mode})
			}
		}
	}

	options := make([]ProfileOption, 0, len(requests))
	for _, req := range requests {
		profile, err := ResolveProfile(req)
		option := ProfileOption{
			Template: req.Template, FrameworkVersion: req.FrameworkVersion, FrontendStack: req.FrontendStack,
			InertiaAdapter: req.InertiaAdapter, ProjectVariant: valueOr(req.ProjectVariant, "empty"), SetupMode: req.SetupMode,
			Enabled: err == nil,
		}
		if err != nil {
			option.Reason = err.Error()
			if req.Template == "laravel" {
				option.MinimumPHP = laravelMinimumPHP[req.FrameworkVersion]
				option.DocumentRoot = "/home/<user>/app/public"
			}
		} else {
			option.MinimumPHP = profile.MinimumPHP
			option.DocumentRoot = "/home/<user>/" + profile.RelativeDocumentRoot
			if profile.RequiresComposer {
				option.Prerequisites = append(option.Prerequisites, "composer")
			}
			if profile.RequiresNode {
				option.Prerequisites = append(option.Prerequisites, "node")
			}
		}
		if req.Template != "static" {
			for _, version := range []string{"8.1", "8.2", "8.3", "8.4"} {
				compatible := option.MinimumPHP == "" || comparePHP(version, option.MinimumPHP) >= 0
				compatibility := CompatibilityOption{Version: version, Enabled: compatible}
				if !compatible {
					compatibility.Reason = fmt.Sprintf("Laravel %s requires PHP %s or newer", req.FrameworkVersion, option.MinimumPHP)
				}
				option.PHPCompatibility = append(option.PHPCompatibility, compatibility)
			}
		}
		options = append(options, option)
	}
	return options
}
