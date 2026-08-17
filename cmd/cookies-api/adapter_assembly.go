package main

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/config"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
)

// storedRoute is what the settings page saved for one capability, flattened to
// the three values an Ark adapter needs. Found is false when nothing is saved
// yet, which is the normal state of a fresh install.
type storedRoute struct {
	BaseURL string
	Model   string
	APIKey  string
	Found   bool
}

// usable reports whether the route can drive an adapter on its own. A route
// whose credential could not be decrypted is treated as absent: taking it would
// leave the capability authenticated by nothing, and the operator would see a
// page that looks configured next to a capability that fails on every call.
func (r storedRoute) usable() bool {
	return r.Found && strings.TrimSpace(r.APIKey) != "" && strings.TrimSpace(r.BaseURL) != ""
}

type envArkText struct {
	BaseURL string
	Model   string
	APIKey  string
}

type envArkImage struct {
	BaseURL string
	Model   string
	APIKey  string
}

// arkTextAdapterConfig picks between the saved configuration and the
// environment. The stored route wins whole — mixing a stored address with an
// environment key would produce a combination nobody ever configured.
func arkTextAdapterConfig(route storedRoute, fallback envArkText) provider.ArkTextConfig {
	if route.usable() {
		return provider.ArkTextConfig{APIKey: route.APIKey, Model: route.Model, BaseURL: route.BaseURL}
	}
	return provider.ArkTextConfig{APIKey: fallback.APIKey, Model: fallback.Model, BaseURL: fallback.BaseURL}
}

func arkImageAdapterConfig(route storedRoute, fallback envArkImage) provider.ArkImageConfig {
	if route.usable() {
		return provider.ArkImageConfig{APIKey: route.APIKey, Model: route.Model, BaseURL: route.BaseURL}
	}
	return provider.ArkImageConfig{APIKey: fallback.APIKey, Model: fallback.Model, BaseURL: fallback.BaseURL}
}

// storedRouteLookupTimeout bounds the boot-time read. A slow database must not
// hold the process at startup; a timeout falls back to the environment, which
// is the behaviour these branches had before.
const storedRouteLookupTimeout = 5 * time.Second

// lookupStoredRoute reads what the settings page saved for one model alias.
// Every failure path returns Found:false rather than an error: this runs during
// assembly, and refusing to boot because no configuration has been saved yet
// would be worse than starting on the environment values.
func lookupStoredRoute(cfg config.Config, db *sql.DB, capability, modelAlias string) storedRoute {
	if db == nil {
		return storedRoute{}
	}
	cipher, err := provider.NewAESGCMCredentialCipher(cfg.Provider.MasterKey, cfg.Provider.MasterKeyVersion)
	if err != nil {
		return storedRoute{}
	}
	store := provider.MySQLGatewayConfigStore{DB: db, Cipher: cipher, AllowInsecureHTTP: cfg.Provider.AllowInsecureHTTP}

	ctx, cancel := context.WithTimeout(context.Background(), storedRouteLookupTimeout)
	defer cancel()

	// These branches only run on a local install, where every request carries
	// the one seeded identity. A globally scoped route (organization_id NULL)
	// matches regardless, so an empty value here is not a miss.
	var organizationID contract.OrganizationID
	if cfg.LocalIdentity != nil {
		organizationID = contract.OrganizationID(cfg.LocalIdentity.OrganizationID)
	}

	var snapshot provider.GatewayRouteSnapshot
	switch capability {
	case "text.generate":
		snapshot, err = store.ResolveTextRoute(ctx, organizationID, modelAlias)
	case "image.generate":
		snapshot, err = store.ResolveImageRoute(ctx, organizationID, modelAlias)
	default:
		return storedRoute{}
	}
	if err != nil {
		return storedRoute{}
	}

	apiKey, err := store.ResolveGatewayCredential(ctx, snapshot.CredentialID, snapshot.CredentialVersion)
	if err != nil {
		// The route exists but its key cannot be read. Say so once at boot:
		// the capability will run on the environment key, and without this line
		// there is nothing to explain why the saved configuration is inert.
		log.Printf("saved %s route found but its credential could not be read, using environment values: %v", capability, err)
		return storedRoute{}
	}
	return storedRoute{BaseURL: snapshot.BaseURL, Model: snapshot.UpstreamModel, APIKey: apiKey, Found: true}
}
