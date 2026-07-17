package provision

import (
	"context"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/config"
)

// defaultProxyMappingMarkers are the "managed" markers of the scope property
// mappings authentik assigns to a proxy provider by default. Resolved to pks by
// resolveProxyMappings. Markers absent on older versions are skipped.
var defaultProxyMappingMarkers = []string{
	"goauthentik.io/providers/proxy/scope-proxy",
	"goauthentik.io/providers/oauth2/scope-openid",
	"goauthentik.io/providers/oauth2/scope-profile",
	"goauthentik.io/providers/oauth2/scope-email",
	"goauthentik.io/providers/oauth2/scope-entitlements",
}

// resolveProxyMappings resolves the default proxy scope mappings to their pks,
// caching the result for the run.
func (p *Provisioner) resolveProxyMappings(ctx context.Context) ([]string, error) {
	if p.proxyMappingPKs != nil {
		return p.proxyMappingPKs, nil
	}
	var list struct {
		Results []struct {
			PK      string `json:"pk"`
			Managed string `json:"managed"`
		} `json:"results"`
	}
	if err := p.api.Get(ctx, "/propertymappings/all/?page_size=1000", &list); err != nil {
		return nil, err
	}
	byMarker := map[string]string{}
	for _, m := range list.Results {
		if m.Managed != "" {
			byMarker[m.Managed] = m.PK
		}
	}
	pks := []string{}
	for _, marker := range defaultProxyMappingMarkers {
		if pk, ok := byMarker[marker]; ok {
			pks = append(pks, pk)
		}
	}
	p.proxyMappingPKs = pks
	return pks, nil
}

// provisionProxy upserts a proxy provider (lookup by name), its application, the
// group access bindings, and — unless disabled — its membership in the embedded
// outpost (without which a proxy provider serves nothing).
func (p *Provisioner) provisionProxy(ctx context.Context, prov *config.ProxyProvider) error {
	log.Printf(">> provisioning %s (proxy/%s)", prov.Name, prov.Mode)

	mappings, err := p.resolveProxyMappings(ctx)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":                         prov.Name,
		"authorization_flow":           p.authFlowPK,
		"invalidation_flow":            p.invalFlowPK,
		"external_host":                prov.ExternalHost,
		"mode":                         prov.Mode,
		"property_mappings":            mappings,
		"internal_host_ssl_validation": boolOrDefault(prov.InternalHostSSLValidation, true),
		"intercept_header_auth":        boolOrDefault(prov.InterceptHeaderAuth, true),
	}
	if prov.InternalHost != "" {
		body["internal_host"] = prov.InternalHost
	}
	if prov.SkipPathRegex != "" {
		body["skip_path_regex"] = prov.SkipPathRegex
	}
	if prov.CookieDomain != "" {
		body["cookie_domain"] = prov.CookieDomain
	}
	if prov.AccessTokenValidity != "" {
		body["access_token_validity"] = prov.AccessTokenValidity
	}

	if p.dryRun {
		log.Printf("   [dry-run] would upsert proxy provider %q and application %q", prov.Name, prov.Slug)
		return nil
	}

	existingPK, err := p.api.FirstPK(ctx, "/providers/proxy/?name="+urlq(prov.Name))
	if err != nil {
		return err
	}
	var resp struct {
		PK int `json:"pk"`
	}
	if existingPK != "" {
		if err := p.api.Patch(ctx, "/providers/proxy/"+existingPK+"/", body, &resp); err != nil {
			return err
		}
	} else {
		if err := p.api.Post(ctx, "/providers/proxy/", body, &resp); err != nil {
			return err
		}
	}

	appName := prov.AppName
	if appName == "" {
		appName = prov.Name
	}
	appBody := map[string]any{"name": appName, "slug": prov.Slug, "provider": resp.PK}
	if prov.Icon != "" {
		appBody["meta_icon"] = prov.Icon
	}
	appPK, err := p.upsertApplication(ctx, prov.Slug, appBody)
	if err != nil {
		return err
	}

	if len(prov.AccessGroups) > 0 {
		if err := p.reconcileAppAccess(ctx, appPK, prov.AccessGroups); err != nil {
			return err
		}
	}

	if boolOrDefault(prov.EmbeddedOutpost, true) {
		if err := p.addToEmbeddedOutpost(ctx, resp.PK); err != nil {
			return err
		}
	}

	log.Printf("   OK %s (proxy provider + application %s)", prov.Name, prov.Slug)
	return nil
}

// addToEmbeddedOutpost adds the provider pk to the embedded outpost's provider
// list (idempotent). The embedded outpost is identified by its managed marker.
func (p *Provisioner) addToEmbeddedOutpost(ctx context.Context, providerPK int) error {
	var list struct {
		Results []struct {
			PK        string `json:"pk"`
			Managed   string `json:"managed"`
			Providers []int  `json:"providers"`
		} `json:"results"`
	}
	if err := p.api.Get(ctx, "/outposts/instances/?page_size=1000", &list); err != nil {
		return err
	}
	for _, o := range list.Results {
		if o.Managed != "goauthentik.io/outposts/embedded" {
			continue
		}
		for _, existing := range o.Providers {
			if existing == providerPK {
				return nil // already a member
			}
		}
		providers := append(o.Providers, providerPK)
		if err := p.api.Patch(ctx, "/outposts/instances/"+o.PK+"/", map[string]any{"providers": providers}, nil); err != nil {
			return err
		}
		log.Printf("   OK added to embedded outpost")
		return nil
	}
	log.Printf("   WARN embedded outpost not found; proxy provider will not be served")
	return nil
}

func boolOrDefault(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}
