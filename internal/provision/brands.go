package provision

import (
	"context"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/config"
)

// provisionBrand upserts an authentik Brand (appearance/theme for a domain).
// Idempotent: brands are matched by domain and PATCHed when present, POSTed
// otherwise. Empty fields are omitted so existing values are preserved.
func (p *Provisioner) provisionBrand(ctx context.Context, b *config.Brand) error {
	log.Printf(">> provisioning brand %s", b.Domain)

	body := map[string]any{
		"domain":  b.Domain,
		"default": b.Default,
	}
	setIfNotEmpty(body, "branding_title", b.Title)
	setIfNotEmpty(body, "branding_logo", b.Logo)
	setIfNotEmpty(body, "branding_favicon", b.Favicon)
	setIfNotEmpty(body, "branding_default_flow_background", b.FlowBackground)
	setIfNotEmpty(body, "branding_custom_css", b.CustomCSS)

	// Resolve optional default flow slugs to their pk.
	flows := map[string]string{
		"flow_authentication": b.AuthenticationFlow,
		"flow_invalidation":   b.InvalidationFlow,
		"flow_recovery":       b.RecoveryFlow,
		"flow_unenrollment":   b.UnenrollmentFlow,
		"flow_user_settings":  b.UserSettingsFlow,
		"flow_device_code":    b.DeviceCodeFlow,
	}
	for field, slug := range flows {
		pk, err := p.flowPK(ctx, slug)
		if err != nil {
			return err
		}
		if pk != "" {
			body[field] = pk
		}
	}

	// Resolve the default application slug to its pk.
	if b.DefaultApplication != "" {
		var app struct {
			PK string `json:"pk"`
		}
		if err := p.api.Get(ctx, "/core/applications/"+b.DefaultApplication+"/", &app); err != nil {
			return err
		}
		body["default_application"] = app.PK
	}

	if len(b.Attributes) > 0 {
		body["attributes"] = b.Attributes
	}

	if p.dryRun {
		log.Printf("   [dry-run] would upsert brand %q (default=%v)", b.Domain, b.Default)
		return nil
	}

	// Brands are keyed by brand_uuid (not "pk").
	pk, err := p.api.FirstValue(ctx, "/core/brands/?domain="+urlq(b.Domain), "brand_uuid")
	if err != nil {
		return err
	}
	if pk != "" {
		if err := p.api.Patch(ctx, "/core/brands/"+pk+"/", body, nil); err != nil {
			return err
		}
	} else {
		if err := p.api.Post(ctx, "/core/brands/", body, nil); err != nil {
			return err
		}
	}
	log.Printf("   OK brand %s", b.Domain)
	return nil
}

// setIfNotEmpty adds key=value to m only when value is non-empty, so PATCH never
// clears an existing brand field that the config leaves blank.
func setIfNotEmpty(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}
