package identity

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestPasswordHashIsSaltedAndVerifiable(t *testing.T) {
	t.Parallel()
	first, err := hashPassword("123456")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPassword("123456")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("password hashes must use independent random salts")
	}
	if !verifyPassword(first, "123456") || verifyPassword(first, "wrong") {
		t.Fatal("password verification accepted the wrong credential")
	}
	if !strings.HasPrefix(string(first), "pbkdf2-sha256$210000$") {
		t.Fatalf("unexpected password hash format: %q", first)
	}
}

func TestPasswordVerificationRejectsMalformedOrWeakEncoding(t *testing.T) {
	t.Parallel()
	for _, value := range [][]byte{
		nil,
		[]byte("pbkdf2-sha256$1$bad$bad"),
		[]byte("bcrypt$210000$bad$bad"),
	} {
		if verifyPassword(value, "123456") {
			t.Fatalf("malformed password hash was accepted: %q", value)
		}
	}
}

func TestSessionCookiesAreHttpOnlyAndSameSiteStrict(t *testing.T) {
	t.Parallel()
	service := PasswordSessionService{Secure: true}
	expires := time.Now().UTC().Add(time.Hour)
	cookie := service.Cookie("opaque-token", expires)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unsafe session cookie: %#v", cookie)
	}
	expired := service.ExpiredCookie()
	if expired.MaxAge != -1 || expired.Value != "" || !expired.HttpOnly || !expired.Secure {
		t.Fatalf("unsafe expired session cookie: %#v", expired)
	}
}

func TestScopesForOrganizationRoleDoesNotGrantManagementToMemberOrAuditor(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"member", "auditor"} {
		scopes, err := ScopesForOrganizationRole(role)
		if err != nil {
			t.Fatalf("ScopesForOrganizationRole(%q) error = %v", role, err)
		}
		actor := contract.ActorContext{Scopes: scopes}
		if actor.HasScope("organization.members.manage") || actor.HasScope("project.members.manage") {
			t.Fatalf("%s unexpectedly received membership management scopes: %v", role, scopes)
		}
	}
}

func TestScopesForOrganizationRoleRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	if _, err := ScopesForOrganizationRole("super-admin"); !errors.Is(err, ErrActorInactive) {
		t.Fatalf("error = %v, want ErrActorInactive", err)
	}
}

func TestAdminSessionCanCreateProviderJobs(t *testing.T) {
	t.Parallel()
	actor := contract.ActorContext{Scopes: adminScopes()}
	if !actor.HasScope("provider.job.create") {
		t.Fatal("admin login must include provider.job.create for image and video generation")
	}
}

func TestOnlyAdministratorsCanConfigureModelServices(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"owner", "admin"} {
		scopes, err := ScopesForOrganizationRole(role)
		if err != nil {
			t.Fatal(err)
		}
		if !(&contract.ActorContext{Scopes: scopes}).HasScope("provider.configuration.write") {
			t.Fatalf("%s must be able to save the video model configuration", role)
		}
	}
	for _, role := range []string{"member", "auditor"} {
		scopes, err := ScopesForOrganizationRole(role)
		if err != nil {
			t.Fatal(err)
		}
		if (&contract.ActorContext{Scopes: scopes}).HasScope("provider.configuration.write") {
			t.Fatalf("%s unexpectedly received the model configuration scope", role)
		}
	}
}

func TestOnlyAdministratorsReceiveDocumentVisionReconciliationScope(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"owner", "admin"} {
		scopes, err := ScopesForOrganizationRole(role)
		if err != nil {
			t.Fatal(err)
		}
		if !(&contract.ActorContext{Scopes: scopes}).HasScope("knowledge.document_vision.reconcile") {
			t.Fatalf("%s must receive document vision reconciliation scope", role)
		}
	}
	for _, role := range []string{"member", "auditor"} {
		scopes, err := ScopesForOrganizationRole(role)
		if err != nil {
			t.Fatal(err)
		}
		if (&contract.ActorContext{Scopes: scopes}).HasScope("knowledge.document_vision.reconcile") {
			t.Fatalf("%s unexpectedly received document vision reconciliation scope", role)
		}
	}
}
