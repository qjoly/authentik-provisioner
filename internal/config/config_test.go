package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaultsAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
oauth2Providers:
  - slug: grafana
    name: Grafana
    redirectURIs:
      - ${GRAFANA_REDIRECT}
  - slug: kargo
    name: Kargo
    clientType: public
    clientID: kargo
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRAFANA_REDIRECT", "https://grafana.example.com/cb")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if cfg.ManagedBy != "gitops-provisioner" {
		t.Errorf("ManagedBy default = %q", cfg.ManagedBy)
	}
	g := cfg.OAuth2Providers[0]
	if g.ClientType != "confidential" {
		t.Errorf("default clientType = %q, want confidential", g.ClientType)
	}
	if got := g.RedirectURIs[0]; got != "https://grafana.example.com/cb" {
		t.Errorf("env expansion failed: %q", got)
	}
	if len(g.Scopes) != 3 || g.SubMode != "user_email" {
		t.Errorf("provider defaults not applied: scopes=%v subMode=%q", g.Scopes, g.SubMode)
	}
}

func TestValidateRejectsPublicWithoutClientID(t *testing.T) {
	cfg := &Config{
		OAuth2Providers: []OAuth2Provider{
			{Slug: "kargo", Name: "Kargo", ClientType: "public"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for public client without clientID")
	}
}

func TestValidateRejectsDuplicateSlug(t *testing.T) {
	cfg := &Config{
		OAuth2Providers: []OAuth2Provider{
			{Slug: "a", Name: "A", ClientType: "confidential"},
			{Slug: "a", Name: "B", ClientType: "confidential"},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for duplicate slug")
	}
}
