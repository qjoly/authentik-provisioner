package provision

import (
	"context"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/config"
)

// provisionOAuthSource upserts a social-login OAuth source and, when requested,
// exposes it as a button on an identification stage. Lookup is by slug.
func (p *Provisioner) provisionOAuthSource(ctx context.Context, src *config.OAuthSource) error {
	log.Printf(">> provisioning social login source %s", src.Name)

	authFlow, err := p.flowPK(ctx, src.AuthenticationFlow)
	if err != nil {
		return err
	}
	enrollFlow, err := p.flowPK(ctx, src.EnrollmentFlow)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":               src.Name,
		"slug":               src.Slug,
		"provider_type":      src.ProviderType,
		"enabled":            true,
		"consumer_key":       src.ConsumerKey,
		"consumer_secret":    src.ConsumerSecret,
		"user_matching_mode": src.UserMatchingMode,
		"authorization_url":  src.AuthorizationURL,
		"access_token_url":   src.AccessTokenURL,
		"profile_url":        src.ProfileURL,
	}
	if src.OIDCJWKSURL != "" {
		body["oidc_jwks_url"] = src.OIDCJWKSURL
	}
	if src.OIDCWellKnownURL != "" {
		body["oidc_well_known_url"] = src.OIDCWellKnownURL
	}
	if authFlow != "" {
		body["authentication_flow"] = authFlow
	}
	if enrollFlow != "" {
		body["enrollment_flow"] = enrollFlow
	}

	if p.dryRun {
		log.Printf("   [dry-run] would upsert source %q", src.Slug)
		return nil
	}

	err = p.api.Get(ctx, "/sources/oauth/"+src.Slug+"/", nil)
	switch {
	case err == nil:
		if err := p.api.Patch(ctx, "/sources/oauth/"+src.Slug+"/", body, nil); err != nil {
			return err
		}
	case isNotFound(err):
		if err := p.api.Post(ctx, "/sources/oauth/", body, nil); err != nil {
			return err
		}
	default:
		return err
	}
	log.Printf("   OK social login source %s", src.Slug)

	if src.AddToLoginStage != "" {
		if err := p.addSourceToStage(ctx, src.Slug, src.AddToLoginStage); err != nil {
			return err
		}
	}
	return nil
}

// addSourceToStage appends the source to an identification stage's `sources`
// list (empty by default, so the login button would never show otherwise).
// Idempotent: the source is appended only when absent.
func (p *Provisioner) addSourceToStage(ctx context.Context, sourceSlug, stageName string) error {
	var source struct {
		PK string `json:"pk"`
	}
	if err := p.api.Get(ctx, "/sources/oauth/"+sourceSlug+"/", &source); err != nil {
		return err
	}

	stagePK, err := p.api.FirstPK(ctx, "/stages/identification/?name="+urlq(stageName))
	if err != nil {
		return err
	}
	if stagePK == "" {
		log.Printf("   WARN identification stage %q not found, cannot expose source button", stageName)
		return nil
	}

	var stage struct {
		Sources []string `json:"sources"`
	}
	if err := p.api.Get(ctx, "/stages/identification/"+stagePK+"/", &stage); err != nil {
		return err
	}
	for _, s := range stage.Sources {
		if s == source.PK {
			return nil // already present
		}
	}
	stage.Sources = append(stage.Sources, source.PK)
	if err := p.api.Patch(ctx, "/stages/identification/"+stagePK+"/", map[string]any{"sources": stage.Sources}, nil); err != nil {
		return err
	}
	log.Printf("   OK %s added to identification stage %q", sourceSlug, stageName)
	return nil
}
