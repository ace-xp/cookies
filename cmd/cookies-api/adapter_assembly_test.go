package main

import "testing"

// The settings page owns text and image configuration. If the local
// self-managed branch keeps reading .env directly, saving in the page appears
// to do nothing on a local install — the exact confusion this work removes.
func TestArkTextAdapterPrefersStoredRoute(t *testing.T) {
	config := arkTextAdapterConfig(
		storedRoute{BaseURL: "https://stored.example.com", Model: "stored-model", APIKey: "sk-stored", Found: true},
		envArkText{BaseURL: "https://env.example.com", Model: "env-model", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://stored.example.com" {
		t.Fatalf("stored route must win, got %q", config.BaseURL)
	}
	if config.Model != "stored-model" {
		t.Fatalf("stored model must win, got %q", config.Model)
	}
	if config.APIKey != "sk-stored" {
		t.Fatalf("stored credential must win, got %q", config.APIKey)
	}
}

func TestArkTextAdapterFallsBackToEnvironment(t *testing.T) {
	config := arkTextAdapterConfig(
		storedRoute{Found: false},
		envArkText{BaseURL: "https://env.example.com", Model: "env-model", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://env.example.com" {
		t.Fatalf("environment fallback must apply, got %q", config.BaseURL)
	}
	if config.Model != "env-model" || config.APIKey != "sk-env" {
		t.Fatalf("environment fallback must carry model and key, got %+v", config)
	}
}

func TestArkImageAdapterPrefersStoredRoute(t *testing.T) {
	config := arkImageAdapterConfig(
		storedRoute{BaseURL: "https://stored.example.com", Model: "seedream-stored", APIKey: "sk-stored", Found: true},
		envArkImage{BaseURL: "https://env.example.com", Model: "seedream-env", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://stored.example.com" || config.Model != "seedream-stored" || config.APIKey != "sk-stored" {
		t.Fatalf("stored route must win, got %+v", config)
	}
}

func TestArkImageAdapterFallsBackToEnvironment(t *testing.T) {
	config := arkImageAdapterConfig(
		storedRoute{Found: false},
		envArkImage{BaseURL: "https://env.example.com", Model: "seedream-env", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://env.example.com" || config.Model != "seedream-env" || config.APIKey != "sk-env" {
		t.Fatalf("environment fallback must apply, got %+v", config)
	}
}

// A route saved without a usable credential must not silently take over the
// environment key: the operator would see a working page and a failing
// capability, with nothing pointing at the cause.
func TestArkTextAdapterIgnoresRouteWithoutCredential(t *testing.T) {
	config := arkTextAdapterConfig(
		storedRoute{BaseURL: "https://stored.example.com", Model: "stored-model", Found: true},
		envArkText{BaseURL: "https://env.example.com", Model: "env-model", APIKey: "sk-env"},
	)
	if config.APIKey != "sk-env" {
		t.Fatalf("a route without a credential must not blank the key, got %q", config.APIKey)
	}
	if config.BaseURL != "https://env.example.com" {
		t.Fatalf("a route without a credential must be ignored whole, got %q", config.BaseURL)
	}
}
