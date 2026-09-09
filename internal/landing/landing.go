package landing

import (
	"bytes"
	_ "embed"
	"html/template"
)

const RobotsTXT = "User-agent: *\nDisallow: /\n"

//go:embed nginx-welcome.html
var nginxWelcomeHTML string

//go:embed landing.css
var landingCSS string

//go:embed website.html
var websiteHTML string

var websiteTemplate = template.Must(template.New("website").Parse(websiteHTML))

// NginxWelcome returns the branded page used by Ubuntu's default Nginx site.
func NginxWelcome() string {
	return nginxWelcomeHTML
}

// CSS returns the locally compiled Tailwind stylesheet used by landing pages.
func CSS() string {
	return landingCSS
}

// WebsiteUnderDevelopment renders the default page for a newly provisioned website.
func WebsiteUnderDevelopment(domain string) (string, error) {
	var output bytes.Buffer
	if err := websiteTemplate.Execute(&output, struct{ Domain string }{Domain: domain}); err != nil {
		return "", err
	}
	return output.String(), nil
}
