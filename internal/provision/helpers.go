package provision

import (
	"errors"
	"fmt"

	"github.com/qjoly/authentik-provisioner/internal/authentik"
)

// urlq is a short alias for query escaping.
func urlq(s string) string { return authentik.QueryEscape(s) }

// isNotFound reports whether err is an authentik 404.
func isNotFound(err error) bool {
	var apiErr *authentik.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == 404
	}
	return false
}

// errNoSecretWriter is returned when a provider wants a Kubernetes secret but
// no writer is available (e.g. --no-secrets or missing cluster access).
func errNoSecretWriter(name string) error {
	return fmt.Errorf("provider requests secret %q but no Kubernetes secret writer is configured", name)
}
