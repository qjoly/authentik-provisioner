package provision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/qjoly/authentik-provisioner/internal/authentik"
	"github.com/qjoly/authentik-provisioner/internal/config"
)

// fakeAuthentik serves the endpoints an OAuth2 provider and a group touch, and
// records the groups bound to applications.
type fakeAuthentik struct {
	t        *testing.T
	mu       sync.Mutex
	groups   map[string]string // name -> pk
	bindings []string          // bound group pks, in creation order
}

func (f *fakeAuthentik) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/api/v3")
	q := r.URL.Query()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	list := func(items ...map[string]any) { reply(map[string]any{"results": items}) }

	switch {
	case r.Method == http.MethodGet && path == "/flows/instances/":
		list(map[string]any{"pk": "flow-" + q.Get("slug")})
	case r.Method == http.MethodGet && path == "/crypto/certificatekeypairs/":
		list(map[string]any{"pk": "key"})
	case r.Method == http.MethodGet && path == "/propertymappings/provider/scope/":
		list(map[string]any{"pk": "scope-" + q.Get("scope_name")})
	case r.Method == http.MethodGet && path == "/providers/oauth2/":
		list()
	case r.Method == http.MethodPost && path == "/providers/oauth2/":
		reply(map[string]any{"pk": 1})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/core/applications/"):
		http.NotFound(w, r)
	case r.Method == http.MethodPost && path == "/core/applications/":
		reply(map[string]any{"pk": "app"})
	case r.Method == http.MethodGet && path == "/core/groups/":
		if pk, ok := f.groups[q.Get("name")]; ok {
			list(map[string]any{"pk": pk})
		} else {
			list()
		}
	case r.Method == http.MethodPost && path == "/core/groups/":
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.groups[body.Name] = "group-" + body.Name
		reply(map[string]any{"pk": f.groups[body.Name]})
	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/core/groups/"):
		reply(map[string]any{})
	case r.Method == http.MethodGet && path == "/policies/bindings/":
		list()
	case r.Method == http.MethodPost && path == "/policies/bindings/":
		var body struct {
			Group string `json:"group"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bindings = append(f.bindings, body.Group)
		reply(map[string]any{})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}
}

// A group declared next to the provider it guards must exist before the
// application's bindings are reconciled, or the first run leaves the
// application open to every user.
func TestRunBindsGroupsCreatedInTheSameRun(t *testing.T) {
	fake := &fakeAuthentik{t: t, groups: map[string]string{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	cfg := &config.Config{
		ManagedBy: "test",
		Defaults:  config.Defaults{AuthorizationFlow: "authz", InvalidationFlow: "inval"},
		OAuth2Providers: []config.OAuth2Provider{{
			Slug:         "app",
			Name:         "App",
			ClientType:   "public",
			ClientID:     "app",
			Scopes:       []string{"openid"},
			AccessGroups: []string{"app-users"},
		}},
		Groups: []config.Group{{Name: "app-users"}},
	}

	if err := New(authentik.New(srv.URL, "token"), cfg, nil, false).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"group-app-users"}; !slices.Equal(fake.bindings, want) {
		t.Fatalf("bindings = %v, want %v", fake.bindings, want)
	}
}
