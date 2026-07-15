package provision

import (
	"context"
	"log"
)

// bindingList is a page of policy bindings with the fields we reconcile on.
type bindingList struct {
	Results []struct {
		PK    string  `json:"pk"`
		Group *string `json:"group"` // group uuid, null for non-group bindings
	} `json:"results"`
}

// reconcileAppAccess makes the given groups authoritative for who can access an
// application: it creates the missing group -> application policy bindings and
// deletes group bindings that are no longer desired. Non-group bindings (user,
// policy, expression) on the same application are left untouched.
//
// Group names that do not resolve are skipped with a warning rather than
// removing every binding (which would lock users out).
func (p *Provisioner) reconcileAppAccess(ctx context.Context, appPK string, groupNames []string) error {
	// Resolve desired group names to pks, preserving order for binding.order.
	desired := map[string]string{} // group pk -> name
	desiredOrder := make([]string, 0, len(groupNames))
	for _, name := range groupNames {
		gpk, err := p.api.FirstPK(ctx, "/core/groups/?name="+urlq(name))
		if err != nil {
			return err
		}
		if gpk == "" {
			log.Printf("   WARN access group %q not found, skipping", name)
			continue
		}
		if _, seen := desired[gpk]; !seen {
			desired[gpk] = name
			desiredOrder = append(desiredOrder, gpk)
		}
	}

	// Existing group bindings for this application.
	var list bindingList
	if err := p.api.Get(ctx, "/policies/bindings/?target="+appPK, &list); err != nil {
		return err
	}
	existing := map[string]string{} // group pk -> binding pk
	for _, b := range list.Results {
		if b.Group != nil && *b.Group != "" {
			existing[*b.Group] = b.PK
		}
	}

	// Create the bindings that are desired but absent.
	for order, gpk := range desiredOrder {
		if _, ok := existing[gpk]; ok {
			continue
		}
		body := map[string]any{
			"target":  appPK,
			"group":   gpk,
			"order":   order,
			"enabled": true,
		}
		if err := p.api.Post(ctx, "/policies/bindings/", body, nil); err != nil {
			return err
		}
		log.Printf("   OK access granted to group %q", desired[gpk])
	}

	// Delete group bindings that are no longer desired.
	for gpk, bpk := range existing {
		if _, ok := desired[gpk]; ok {
			continue
		}
		if err := p.api.Delete(ctx, "/policies/bindings/"+bpk+"/"); err != nil {
			return err
		}
		log.Printf("   removed stale access binding (group=%s)", gpk)
	}
	return nil
}
