// Command provisioner applies a desired-state YAML config against an authentik
// instance. It is idempotent and safe to run repeatedly (as a script or a
// Kubernetes Job).
//
// Required environment:
//
//	AUTHENTIK_URL    public authentik URL (without /api/v3)
//	AUTHENTIK_TOKEN  API token of an admin service account
//
// Flags:
//
//	-config      path to the YAML config (default "config.yaml")
//	-dry-run     log intended changes without calling the API
//	-no-secrets  skip Kubernetes secret writes even if providers request them
//	-wait        wait for the authentik API to be ready before provisioning
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qjoly/authentik-provisioner/internal/authentik"
	"github.com/qjoly/authentik-provisioner/internal/config"
	"github.com/qjoly/authentik-provisioner/internal/kube"
	"github.com/qjoly/authentik-provisioner/internal/provision"
)

func main() {
	log.SetFlags(0)

	var (
		configPath = flag.String("config", "config.yaml", "path to the YAML config file")
		dryRun     = flag.Bool("dry-run", false, "log intended changes without calling the authentik API")
		noSecrets  = flag.Bool("no-secrets", false, "do not write Kubernetes secrets even when providers request them")
		wait       = flag.Bool("wait", true, "wait for the authentik API to be ready before provisioning")
	)
	flag.Parse()

	baseURL := os.Getenv("AUTHENTIK_URL")
	token := os.Getenv("AUTHENTIK_TOKEN")
	if baseURL == "" || token == "" {
		log.Fatal("AUTHENTIK_URL and AUTHENTIK_TOKEN must be set")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := authentik.New(baseURL, token)

	if *wait {
		log.Printf(">> waiting for the authentik API (%s)...", baseURL)
		if err := api.WaitReady(ctx, 5*time.Second); err != nil {
			log.Fatalf("authentik API not ready: %v", err)
		}
	}

	secrets := newSecretWriter(cfg, *dryRun, *noSecrets)

	p := provision.New(api, cfg, secrets, *dryRun)
	if err := p.Run(ctx); err != nil {
		log.Fatalf("provisioning failed: %v", err)
	}
}

// newSecretWriter builds a Kubernetes secret writer only when at least one
// confidential provider declares a secretName (and secrets are not disabled).
// This keeps local/dry-run usage free of any cluster requirement.
func newSecretWriter(cfg *config.Config, dryRun, noSecrets bool) provision.SecretUpserter {
	if dryRun || noSecrets || !needsSecrets(cfg) {
		return nil
	}
	w, err := kube.NewSecretWriter()
	if err != nil {
		log.Fatalf("kubernetes secret writer: %v", err)
	}
	return w
}

func needsSecrets(cfg *config.Config) bool {
	for _, p := range cfg.OAuth2Providers {
		if p.ClientType == "confidential" && p.SecretName != "" {
			return true
		}
	}
	return false
}
