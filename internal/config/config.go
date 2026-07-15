// Package config defines the desired-state schema consumed by the provisioner
// and loads it from a YAML file (with ${ENV} expansion).
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level desired state.
type Config struct {
	// ManagedBy is stamped on users/groups created by the provisioner
	// (attributes.managed_by) so the prune step only ever touches objects it
	// owns. Defaults to "gitops-provisioner".
	ManagedBy string `yaml:"managedBy"`

	// Defaults applied to every OAuth2 provider unless overridden.
	Defaults Defaults `yaml:"defaults"`

	// Bootstrap optionally (re)sets the akadmin password/email.
	Bootstrap *Bootstrap `yaml:"bootstrap"`

	OAuth2Providers []OAuth2Provider `yaml:"oauth2Providers"`
	OAuthSources    []OAuthSource    `yaml:"oauthSources"`
	Users           []User           `yaml:"users"`
	Groups          []Group          `yaml:"groups"`

	// PruneGroups deletes managed groups that are absent from Groups. Users are
	// never pruned (login/data continuity).
	PruneGroups bool `yaml:"pruneGroups"`
}

// Defaults hold values shared by providers.
type Defaults struct {
	// Flow slugs. Sensible authentik built-in defaults are used when empty.
	AuthorizationFlow string   `yaml:"authorizationFlow"`
	InvalidationFlow  string   `yaml:"invalidationFlow"`
	Scopes            []string `yaml:"scopes"`
	SubMode           string   `yaml:"subMode"`
}

// Bootstrap configures the first user (akadmin).
type Bootstrap struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Email    string `yaml:"email"`
}

// OAuth2Provider describes an OAuth2/OIDC provider and its application.
type OAuth2Provider struct {
	Slug         string   `yaml:"slug"`
	Name         string   `yaml:"name"`
	RedirectURIs []string `yaml:"redirectURIs"`
	// ClientType is "confidential" (default) or "public" (PKCE, no secret).
	ClientType string `yaml:"clientType"`
	// ClientID is required for public clients (fixed, non-sensitive id).
	ClientID string   `yaml:"clientID"`
	Scopes   []string `yaml:"scopes"`
	SubMode  string   `yaml:"subMode"`
	Icon     string   `yaml:"icon"`
	// AuthenticationFlow is an optional per-provider auth flow slug.
	AuthenticationFlow string `yaml:"authenticationFlow"`
	// SecretName, when set on a confidential client, writes client_id and
	// client_secret into a Kubernetes secret of that name.
	SecretName string `yaml:"secretName"`
	// AccessGroups restricts the application to members of these groups (by
	// name) via policy bindings. When set, the list is authoritative: group
	// bindings absent from it are removed. When empty/omitted the application's
	// access bindings are left untouched (never locks everyone out).
	AccessGroups []string `yaml:"accessGroups"`
}

// OAuthSource is a social-login source (e.g. an OIDC IdP).
type OAuthSource struct {
	Slug               string `yaml:"slug"`
	Name               string `yaml:"name"`
	ProviderType       string `yaml:"providerType"`
	ConsumerKey        string `yaml:"consumerKey"`
	ConsumerSecret     string `yaml:"consumerSecret"`
	UserMatchingMode   string `yaml:"userMatchingMode"`
	AuthorizationURL   string `yaml:"authorizationURL"`
	AccessTokenURL     string `yaml:"accessTokenURL"`
	ProfileURL         string `yaml:"profileURL"`
	OIDCJWKSURL        string `yaml:"oidcJwksURL"`
	OIDCWellKnownURL   string `yaml:"oidcWellKnownURL"`
	AuthenticationFlow string `yaml:"authenticationFlow"` // slug
	EnrollmentFlow     string `yaml:"enrollmentFlow"`     // slug
	// AddToLoginStage exposes the source as a button on the given
	// identification stage (by name).
	AddToLoginStage string `yaml:"addToLoginStage"`
}

// User is an internal user pre-created so group membership works before first
// login.
type User struct {
	Email string `yaml:"email"`
	Name  string `yaml:"name"`
}

// Group is a managed group with an authoritative member list (by email).
type Group struct {
	Name    string   `yaml:"name"`
	Members []string `yaml:"members"`
}

// Load reads, expands ${ENV} references, and parses the config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	expanded := os.Expand(string(raw), func(key string) string {
		return os.Getenv(key)
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.ManagedBy == "" {
		c.ManagedBy = "gitops-provisioner"
	}
	if c.Defaults.AuthorizationFlow == "" {
		c.Defaults.AuthorizationFlow = "default-provider-authorization-implicit-consent"
	}
	if c.Defaults.InvalidationFlow == "" {
		c.Defaults.InvalidationFlow = "default-provider-invalidation-flow"
	}
	if len(c.Defaults.Scopes) == 0 {
		c.Defaults.Scopes = []string{"openid", "email", "profile"}
	}
	if c.Defaults.SubMode == "" {
		c.Defaults.SubMode = "user_email"
	}

	for i := range c.OAuth2Providers {
		p := &c.OAuth2Providers[i]
		if p.ClientType == "" {
			p.ClientType = "confidential"
		}
		if len(p.Scopes) == 0 {
			p.Scopes = c.Defaults.Scopes
		}
		if p.SubMode == "" {
			p.SubMode = c.Defaults.SubMode
		}
	}
}

// Validate performs light structural checks.
func (c *Config) Validate() error {
	seenSlug := map[string]bool{}
	for _, p := range c.OAuth2Providers {
		if p.Slug == "" || p.Name == "" {
			return fmt.Errorf("oauth2 provider requires slug and name (got slug=%q name=%q)", p.Slug, p.Name)
		}
		if seenSlug[p.Slug] {
			return fmt.Errorf("duplicate oauth2 provider slug %q", p.Slug)
		}
		seenSlug[p.Slug] = true
		if p.ClientType == "public" && p.ClientID == "" {
			return fmt.Errorf("public client %q requires clientID", p.Slug)
		}
		if p.ClientType != "public" && p.ClientType != "confidential" {
			return fmt.Errorf("provider %q has invalid clientType %q", p.Slug, p.ClientType)
		}
	}
	for _, g := range c.Groups {
		if g.Name == "" {
			return fmt.Errorf("group requires a name")
		}
	}
	return nil
}
