package provision

import (
	"context"
	"log"

	"github.com/qjoly/authentik-provisioner/internal/config"
)

// bootstrapAdmin (re)sets the first user's password and email. authentik
// creates the account at startup; the password is applied here rather than via
// AUTHENTIK_BOOTSTRAP_* so it can be sourced from a secret. No-op if the user is
// absent or no password is provided.
func (p *Provisioner) bootstrapAdmin(ctx context.Context, b *config.Bootstrap) error {
	if b.Password == "" {
		return nil
	}
	username := b.Username
	if username == "" {
		username = "akadmin"
	}
	log.Printf(">> initializing first user (%s)", username)

	pk, err := p.api.FirstPK(ctx, "/core/users/?username="+urlq(username))
	if err != nil {
		return err
	}
	if pk == "" {
		log.Printf("   %s not found, skipping", username)
		return nil
	}

	if p.dryRun {
		log.Printf("   [dry-run] would set %s password and email", username)
		return nil
	}

	if err := p.api.Post(ctx, "/core/users/"+pk+"/set_password/", map[string]any{"password": b.Password}, nil); err != nil {
		return err
	}
	patch := map[string]any{"is_active": true}
	if b.Email != "" {
		patch["email"] = b.Email
	}
	if err := p.api.Patch(ctx, "/core/users/"+pk+"/", patch, nil); err != nil {
		return err
	}
	log.Printf("   OK %s password set (email=%s)", username, b.Email)
	return nil
}
