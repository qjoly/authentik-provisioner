package provision

import (
	"context"
	"log"
	"strings"
)

// provisionUser pre-creates an internal user (idempotent, lookup by email) so
// group membership can reference it before the user's first login. The user is
// stamped with the managed_by marker.
func (p *Provisioner) provisionUser(ctx context.Context, email, name string) error {
	log.Printf(">> provisioning user %s", email)
	if name == "" {
		name = displayNameFromEmail(email)
	}
	username := email
	if i := strings.Index(email, "@"); i > 0 {
		username = email[:i]
	}

	pk, err := p.api.FirstPK(ctx, "/core/users/?email="+urlq(email))
	if err != nil {
		return err
	}

	if p.dryRun {
		log.Printf("   [dry-run] would ensure user %s (username=%s)", email, username)
		return nil
	}

	if pk == "" {
		body := map[string]any{
			"username":   username,
			"name":       name,
			"email":      email,
			"type":       "internal",
			"is_active":  true,
			"path":       "users",
			"attributes": map[string]any{"managed_by": p.cfg.ManagedBy},
		}
		if err := p.api.Post(ctx, "/core/users/", body, nil); err != nil {
			return err
		}
		log.Printf("   OK user created (%s)", username)
		return nil
	}

	// Merge the marker onto the existing user, keeping other attributes.
	var current struct {
		Attributes map[string]any `json:"attributes"`
	}
	if err := p.api.Get(ctx, "/core/users/"+pk+"/", &current); err != nil {
		return err
	}
	if current.Attributes == nil {
		current.Attributes = map[string]any{}
	}
	current.Attributes["managed_by"] = p.cfg.ManagedBy
	if err := p.api.Patch(ctx, "/core/users/"+pk+"/", map[string]any{"attributes": current.Attributes}, nil); err != nil {
		return err
	}
	log.Printf("   OK user already exists (pk=%s, marked managed)", pk)
	return nil
}

// provisionGroup ensures the group exists then sets its members to the resolved
// emails. PATCH replaces the member list, so the config is authoritative for
// membership. Unresolvable emails are skipped with a warning.
func (p *Provisioner) provisionGroup(ctx context.Context, name string, members []string) error {
	log.Printf(">> provisioning group %s", name)

	if p.dryRun {
		log.Printf("   [dry-run] would ensure group %s with %d member(s)", name, len(members))
		return nil
	}

	pk, err := p.api.FirstPK(ctx, "/core/groups/?name="+urlq(name))
	if err != nil {
		return err
	}
	if pk == "" {
		var created struct {
			PK string `json:"pk"`
		}
		body := map[string]any{
			"name":       name,
			"attributes": map[string]any{"managed_by": p.cfg.ManagedBy},
		}
		if err := p.api.Post(ctx, "/core/groups/", body, &created); err != nil {
			return err
		}
		pk = created.PK
	}

	userPKs := make([]string, 0, len(members))
	for _, email := range members {
		upk, err := p.api.FirstPK(ctx, "/core/users/?email="+urlq(email))
		if err != nil {
			return err
		}
		if upk == "" {
			log.Printf("   WARN user %s not found (not logged in yet?), skipping", email)
			continue
		}
		userPKs = append(userPKs, upk)
	}

	body := map[string]any{
		"users":      userPKs,
		"attributes": map[string]any{"managed_by": p.cfg.ManagedBy},
	}
	if err := p.api.Patch(ctx, "/core/groups/"+pk+"/", body, nil); err != nil {
		return err
	}
	log.Printf("   OK group %s (%d member(s))", name, len(userPKs))
	return nil
}

// groupList is a page of groups with the attributes we filter on.
type groupList struct {
	Results []struct {
		PK         string `json:"pk"`
		Name       string `json:"name"`
		Attributes struct {
			ManagedBy string `json:"managed_by"`
		} `json:"attributes"`
	} `json:"results"`
}

// pruneGroups deletes managed groups (marker == ManagedBy) that are absent from
// the desired config. Only marked groups are ever touched, so built-ins are
// safe. Users are intentionally never pruned.
func (p *Provisioner) pruneGroups(ctx context.Context) error {
	log.Println(">> pruning managed groups absent from the config")

	desired := map[string]bool{}
	for _, g := range p.cfg.Groups {
		desired[g.Name] = true
	}

	var list groupList
	if err := p.api.Get(ctx, "/core/groups/?page_size=1000", &list); err != nil {
		return err
	}
	for _, g := range list.Results {
		if g.Attributes.ManagedBy != p.cfg.ManagedBy {
			continue
		}
		if desired[g.Name] {
			continue
		}
		if p.dryRun {
			log.Printf("   [dry-run] would prune group %s", g.Name)
			continue
		}
		if err := p.api.Delete(ctx, "/core/groups/"+g.PK+"/"); err != nil {
			return err
		}
		log.Printf("   pruned group %s", g.Name)
	}
	return nil
}

// displayNameFromEmail turns "john.doe@x" into "John Doe".
func displayNameFromEmail(email string) string {
	local := email
	if i := strings.Index(email, "@"); i > 0 {
		local = email[:i]
	}
	parts := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
