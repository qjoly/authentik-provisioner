// Package provision applies a desired-state config against an authentik
// instance. Every operation is idempotent: objects are looked up by their
// natural key (slug/name/scope) and PATCHed when present, POSTed otherwise.
package provision

import (
	"context"
	"fmt"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/authentik"
	"github.com/qjoly/authentik-provisioner/internal/config"
)

// SecretUpserter is satisfied by *kube.SecretWriter; it lets the provisioner
// stay decoupled from Kubernetes for dry-run / no-secret runs.
type SecretUpserter interface {
	Upsert(ctx context.Context, name string, data map[string]string) error
	Namespace() string
}

// Provisioner ties together the API client, the config and (optionally) a
// secret writer.
type Provisioner struct {
	api     *authentik.Client
	cfg     *config.Config
	secrets SecretUpserter
	dryRun  bool

	// Resolved once per run and reused by every provider.
	authFlowPK      string
	invalFlowPK     string
	signingKey      string
	scopePK         map[string]string
	proxyMappingPKs []string
}

// New builds a Provisioner. secrets may be nil when no provider needs a
// Kubernetes secret (or in dry-run mode).
func New(api *authentik.Client, cfg *config.Config, secrets SecretUpserter, dryRun bool) *Provisioner {
	return &Provisioner{
		api:     api,
		cfg:     cfg,
		secrets: secrets,
		dryRun:  dryRun,
		scopePK: map[string]string{},
	}
}

// Run executes the full provisioning in the same order as the reference shell
// job: bootstrap, resolve shared objects, providers, sources, users, groups,
// prune.
func (p *Provisioner) Run(ctx context.Context) error {
	if p.cfg.Bootstrap != nil {
		if err := p.bootstrapAdmin(ctx, p.cfg.Bootstrap); err != nil {
			return fmt.Errorf("bootstrap admin: %w", err)
		}
	}

	if len(p.cfg.OAuth2Providers) > 0 || len(p.cfg.ProxyProviders) > 0 {
		if err := p.resolveShared(ctx); err != nil {
			return fmt.Errorf("resolve shared objects: %w", err)
		}
	}

	for i := range p.cfg.OAuth2Providers {
		if err := p.provisionOAuth2(ctx, &p.cfg.OAuth2Providers[i]); err != nil {
			return fmt.Errorf("provision provider %q: %w", p.cfg.OAuth2Providers[i].Slug, err)
		}
	}

	for i := range p.cfg.ProxyProviders {
		if err := p.provisionProxy(ctx, &p.cfg.ProxyProviders[i]); err != nil {
			return fmt.Errorf("provision proxy provider %q: %w", p.cfg.ProxyProviders[i].Slug, err)
		}
	}

	for i := range p.cfg.OAuthSources {
		if err := p.provisionOAuthSource(ctx, &p.cfg.OAuthSources[i]); err != nil {
			return fmt.Errorf("provision source %q: %w", p.cfg.OAuthSources[i].Slug, err)
		}
	}

	for i := range p.cfg.Brands {
		if err := p.provisionBrand(ctx, &p.cfg.Brands[i]); err != nil {
			return fmt.Errorf("provision brand %q: %w", p.cfg.Brands[i].Domain, err)
		}
	}

	for i := range p.cfg.Users {
		u := p.cfg.Users[i]
		if err := p.provisionUser(ctx, u.Email, u.Name); err != nil {
			return fmt.Errorf("provision user %q: %w", u.Email, err)
		}
	}

	for i := range p.cfg.Groups {
		g := p.cfg.Groups[i]
		if err := p.provisionGroup(ctx, g.Name, g.Members); err != nil {
			return fmt.Errorf("provision group %q: %w", g.Name, err)
		}
	}

	if p.cfg.PruneGroups {
		if err := p.pruneGroups(ctx); err != nil {
			return fmt.Errorf("prune groups: %w", err)
		}
	}

	log.Println(">> provisioning done")
	return nil
}

// resolveShared looks up the flows, scope mappings and signing key shared by
// every OAuth2 provider.
func (p *Provisioner) resolveShared(ctx context.Context) error {
	log.Println(">> resolving default flows, scope mappings and signing key")

	var err error
	p.authFlowPK, err = p.api.FirstPK(ctx, "/flows/instances/?slug="+authentik.QueryEscape(p.cfg.Defaults.AuthorizationFlow))
	if err != nil {
		return err
	}
	if p.authFlowPK == "" {
		return fmt.Errorf("authorization flow %q not found", p.cfg.Defaults.AuthorizationFlow)
	}

	p.invalFlowPK, err = p.api.FirstPK(ctx, "/flows/instances/?slug="+authentik.QueryEscape(p.cfg.Defaults.InvalidationFlow))
	if err != nil {
		return err
	}
	if p.invalFlowPK == "" {
		return fmt.Errorf("invalidation flow %q not found", p.cfg.Defaults.InvalidationFlow)
	}

	// RSA signing key (first keypair that has a private key). Without it
	// authentik signs id_tokens with HS256 and RS256 clients reject them.
	p.signingKey, err = p.api.FirstPK(ctx, "/crypto/certificatekeypairs/?has_key=true")
	if err != nil {
		return err
	}
	if p.signingKey == "" {
		log.Println("   WARN no signing key with a private key found; id_tokens will use HS256")
	}
	return nil
}

// scope resolves and caches the pk of an OAuth2 scope property mapping.
func (p *Provisioner) scope(ctx context.Context, name string) (string, error) {
	if pk, ok := p.scopePK[name]; ok {
		return pk, nil
	}
	pk, err := p.api.FirstPK(ctx, "/propertymappings/provider/scope/?scope_name="+authentik.QueryEscape(name))
	if err != nil {
		return "", err
	}
	if pk == "" {
		return "", fmt.Errorf("scope mapping %q not found", name)
	}
	p.scopePK[name] = pk
	return pk, nil
}

// flowPK resolves a flow slug to its pk (used for optional per-provider flows).
func (p *Provisioner) flowPK(ctx context.Context, slug string) (string, error) {
	if slug == "" {
		return "", nil
	}
	pk, err := p.api.FirstPK(ctx, "/flows/instances/?slug="+authentik.QueryEscape(slug))
	if err != nil {
		return "", err
	}
	if pk == "" {
		return "", fmt.Errorf("flow %q not found", slug)
	}
	return pk, nil
}
