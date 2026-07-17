package provision

import (
	"context"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/config"
)

// providerResponse captures the fields we read back after upserting a provider.
type providerResponse struct {
	PK           int    `json:"pk"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// provisionOAuth2 upserts an OAuth2 provider, its application, and (for
// confidential clients with a secretName) the Kubernetes secret holding the
// resulting client_id/client_secret.
func (p *Provisioner) provisionOAuth2(ctx context.Context, prov *config.OAuth2Provider) error {
	log.Printf(">> provisioning %s (%s)", prov.Name, prov.ClientType)

	scopePKs := make([]string, 0, len(prov.Scopes))
	for _, s := range prov.Scopes {
		pk, err := p.scope(ctx, s)
		if err != nil {
			return err
		}
		scopePKs = append(scopePKs, pk)
	}

	authFlow, err := p.flowPK(ctx, prov.AuthenticationFlow)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":               prov.Name,
		"authorization_flow": p.authFlowPK,
		"invalidation_flow":  p.invalFlowPK,
		"client_type":        prov.ClientType,
		"redirect_uris":      redirectURIs(prov.RedirectURIs),
		"property_mappings":  scopePKs,
		"sub_mode":           prov.SubMode,
		"grant_types":        []string{"authorization_code", "refresh_token"},
	}
	if p.signingKey != "" {
		body["signing_key"] = p.signingKey
	}
	if prov.ClientType == "public" {
		body["client_id"] = prov.ClientID
	}
	if authFlow != "" {
		body["authentication_flow"] = authFlow
	}

	if p.dryRun {
		log.Printf("   [dry-run] would upsert provider %q and application %q", prov.Name, prov.Slug)
		return nil
	}

	// Upsert the provider (lookup by name).
	existingPK, err := p.api.FirstPK(ctx, "/providers/oauth2/?name="+urlq(prov.Name))
	if err != nil {
		return err
	}
	var provResp providerResponse
	if existingPK != "" {
		if err := p.api.Patch(ctx, "/providers/oauth2/"+existingPK+"/", body, &provResp); err != nil {
			return err
		}
	} else {
		if err := p.api.Post(ctx, "/providers/oauth2/", body, &provResp); err != nil {
			return err
		}
	}

	// Upsert the application bound to the provider (lookup by slug).
	appName := prov.AppName
	if appName == "" {
		appName = prov.Name
	}
	appBody := map[string]any{
		"name":     appName,
		"slug":     prov.Slug,
		"provider": provResp.PK,
	}
	if prov.Icon != "" {
		appBody["meta_icon"] = prov.Icon
	}
	appPK, err := p.upsertApplication(ctx, prov.Slug, appBody)
	if err != nil {
		return err
	}

	// Reconcile which groups may access the application (policy bindings).
	if len(prov.AccessGroups) > 0 {
		if err := p.reconcileAppAccess(ctx, appPK, prov.AccessGroups); err != nil {
			return err
		}
	}

	// Write the client credentials to Kubernetes for confidential clients.
	if prov.ClientType == "confidential" && prov.SecretName != "" {
		if p.secrets == nil {
			return errNoSecretWriter(prov.SecretName)
		}
		data := map[string]string{
			"client_id":     provResp.ClientID,
			"client_secret": provResp.ClientSecret,
		}
		if err := p.secrets.Upsert(ctx, prov.SecretName, data); err != nil {
			return err
		}
		log.Printf("   OK %s (client_id=%s, k8s secret=%s/%s)", prov.Name, provResp.ClientID, p.secrets.Namespace(), prov.SecretName)
		return nil
	}

	if prov.ClientType == "public" {
		log.Printf("   OK %s (public client_id=%s)", prov.Name, prov.ClientID)
	} else {
		log.Printf("   OK %s (client_id=%s)", prov.Name, provResp.ClientID)
	}
	return nil
}

// upsertApplication PATCHes an existing application (found by slug) or POSTs a
// new one, returning the application's pk (uuid). authentik exposes
// applications at a slug-addressed detail endpoint.
func (p *Provisioner) upsertApplication(ctx context.Context, slug string, body map[string]any) (string, error) {
	var resp struct {
		PK string `json:"pk"`
	}
	err := p.api.Get(ctx, "/core/applications/"+slug+"/", nil)
	if err == nil {
		if err := p.api.Patch(ctx, "/core/applications/"+slug+"/", body, &resp); err != nil {
			return "", err
		}
		return resp.PK, nil
	}
	if !isNotFound(err) {
		return "", err
	}
	if err := p.api.Post(ctx, "/core/applications/", body, &resp); err != nil {
		return "", err
	}
	return resp.PK, nil
}

func redirectURIs(uris []string) []map[string]string {
	out := make([]map[string]string, 0, len(uris))
	for _, u := range uris {
		// authentik >= 2024.2 models redirect URIs as {url, matching_mode}.
		out = append(out, map[string]string{"url": u, "matching_mode": "strict"})
	}
	return out
}
