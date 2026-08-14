package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
)

const SessionCookieName = "cookies_session"
const passwordHashIterations = 210000

var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrCredentialLocked = errors.New("credential temporarily locked")

type PasswordSessionService struct {
	DB         *sql.DB
	Validator  ActorValidator
	UserScopes UserScopeResolver
	SessionTTL time.Duration
	Secure     bool
	Now        func() time.Time
	NewID      ids.Generator
}

type UserScopeResolver interface {
	ResolveUserScopes(context.Context, contract.OrganizationID, string) ([]contract.Scope, error)
}

type LoginResult struct {
	Actor     contract.ActorContext `json:"actor"`
	Token     string                `json:"-"`
	ExpiresAt time.Time             `json:"expires_at"`
}

func (s PasswordSessionService) EnsureBootstrapCredential(ctx context.Context, actor contract.ActorContext, username, password string) error {
	if s.DB == nil {
		return fmt.Errorf("identity database is required")
	}
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return fmt.Errorf("bootstrap username and password are required")
	}
	if err := actor.Validate(); err != nil || actor.Principal.Kind != contract.PrincipalUser {
		return fmt.Errorf("bootstrap actor must be a valid user")
	}
	var exists int
	err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM identity_password_credentials
		WHERE username_normalized = ?`, normalizeUsername(username)).Scan(&exists)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO identity_password_credentials
		(organization_id, user_id, username, username_normalized, password_hash)
		VALUES (?, ?, ?, ?, ?)`,
		actor.OrganizationID, actor.Principal.ID, username, normalizeUsername(username), hash)
	if duplicateKey(err) {
		return nil
	}
	return err
}

func (s PasswordSessionService) Login(ctx context.Context, username, password string) (LoginResult, error) {
	if s.DB == nil || s.Validator == nil {
		return LoginResult{}, fmt.Errorf("identity session dependencies are incomplete")
	}
	now := s.now()
	var organizationID contract.OrganizationID
	var userID string
	var hash []byte
	var failed int
	var lockedUntil sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT organization_id, user_id, password_hash, failed_attempts, locked_until
		FROM identity_password_credentials WHERE username_normalized = ?`, normalizeUsername(username)).
		Scan(&organizationID, &userID, &hash, &failed, &lockedUntil)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			burnUnknownCredential(password)
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return LoginResult{}, ErrCredentialLocked
	}
	if !verifyPassword(hash, password) {
		failed++
		var lock any
		if failed >= 5 {
			lock = now.Add(5 * time.Minute)
			failed = 0
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE identity_password_credentials
			SET failed_attempts = ?, locked_until = ? WHERE organization_id = ? AND user_id = ?`,
			failed, lock, organizationID, userID)
		return LoginResult{}, ErrInvalidCredentials
	}
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
	}
	actor.Scopes, err = s.resolveUserScopes(ctx, organizationID, userID)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := s.Validator.ValidateActor(ctx, actor); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return LoginResult{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	sessionID, err := s.newID("session")
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := now.Add(s.sessionTTL())
	scopes, _ := json.Marshal(actor.Scopes)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE identity_password_credentials
		SET failed_attempts = 0, locked_until = NULL WHERE organization_id = ? AND user_id = ?`,
		organizationID, userID); err != nil {
		return LoginResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions
		(id, organization_id, user_id, token_hash, scopes, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, organizationID, userID, hex.EncodeToString(tokenHash[:]), scopes, expiresAt, now, now); err != nil {
		return LoginResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Actor: actor, Token: token, ExpiresAt: expiresAt}, nil
}

func (s PasswordSessionService) Logout(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" || s.DB == nil {
		return nil
	}
	sum := sha256.Sum256([]byte(token))
	_, err := s.DB.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at = ?
		WHERE token_hash = ? AND revoked_at IS NULL`, s.now(), hex.EncodeToString(sum[:]))
	return err
}

func (s PasswordSessionService) SwitchOrganization(ctx context.Context, token string, organizationID contract.OrganizationID) (LoginResult, error) {
	if s.DB == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(string(organizationID)) == "" {
		return LoginResult{}, ErrUnauthenticated
	}
	now := s.now()
	oldHash := sha256.Sum256([]byte(token))
	var userID string
	err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM identity_sessions
		WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
		hex.EncodeToString(oldHash[:]), now).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, ErrUnauthenticated
	}
	if err != nil {
		return LoginResult{}, err
	}
	scopes, err := s.resolveUserScopes(ctx, organizationID, userID)
	if err != nil {
		return LoginResult{}, ErrActorInactive
	}
	actor := contract.ActorContext{
		OrganizationID: organizationID,
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: userID},
		Scopes:         scopes,
	}
	if err := actor.Validate(); err != nil {
		return LoginResult{}, ErrUnauthenticated
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return LoginResult{}, err
	}
	nextToken := hex.EncodeToString(tokenBytes)
	nextHash := sha256.Sum256([]byte(nextToken))
	sessionID, err := s.newID("session")
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := now.Add(s.sessionTTL())
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return LoginResult{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at = ?
		WHERE token_hash = ? AND revoked_at IS NULL`, now, hex.EncodeToString(oldHash[:]))
	if err != nil {
		return LoginResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return LoginResult{}, ErrUnauthenticated
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions
		(id, organization_id, user_id, token_hash, scopes, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, organizationID, userID, hex.EncodeToString(nextHash[:]), scopesJSON, expiresAt, now, now); err != nil {
		return LoginResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Actor: actor, Token: nextToken, ExpiresAt: expiresAt}, nil
}

func (s PasswordSessionService) Authenticate(ctx context.Context, request *http.Request) (contract.ActorContext, error) {
	if request == nil || s.DB == nil || s.Validator == nil {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	sum := sha256.Sum256([]byte(cookie.Value))
	var actor contract.ActorContext
	var scopesJSON []byte
	err = s.DB.QueryRowContext(ctx, `SELECT organization_id, user_id, scopes
		FROM identity_sessions WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
		hex.EncodeToString(sum[:]), s.now()).Scan(&actor.OrganizationID, &actor.Principal.ID, &scopesJSON)
	if err != nil {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	actor.Principal.Kind = contract.PrincipalUser
	if json.Unmarshal(scopesJSON, &actor.Scopes) != nil || actor.Validate() != nil {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	actor.Scopes, err = s.resolveUserScopes(ctx, actor.OrganizationID, actor.Principal.ID)
	if err != nil {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	if err := s.Validator.ValidateActor(ctx, actor); err != nil {
		return contract.ActorContext{}, ErrUnauthenticated
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE identity_sessions SET last_seen_at = ?
		WHERE token_hash = ?`, s.now(), hex.EncodeToString(sum[:]))
	return actor, nil
}

func (s PasswordSessionService) Cookie(token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteStrictMode,
	}
}

func (s PasswordSessionService) ExpiredCookie() *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: s.Secure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (s PasswordSessionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s PasswordSessionService) sessionTTL() time.Duration {
	if s.SessionTTL > 0 {
		return s.SessionTTL
	}
	return 8 * time.Hour
}

func (s PasswordSessionService) newID(prefix string) (string, error) {
	if s.NewID != nil {
		return s.NewID(prefix)
	}
	return ids.New(prefix)
}

func (s PasswordSessionService) resolveUserScopes(ctx context.Context, organizationID contract.OrganizationID, userID string) ([]contract.Scope, error) {
	if s.UserScopes == nil {
		return nil, ErrUnauthenticated
	}
	return s.UserScopes.ResolveUserScopes(ctx, organizationID, userID)
}

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func adminScopes() []contract.Scope {
	values := []string{
		"identity.profile.write", "organization.read", "organization.members.read",
		"organization.members.manage", "project.members.read", "project.members.manage",
		"project.read", "project.write", "assets.read", "assets.write",
		"provider.read", "provider.generate", "provider.job.create", "provider.text.generate",
		"strategy.read", "strategy.write", "strategy.confirm", "strategy.review",
		"strategy.approve", "strategy.package.read", "creative.read", "creative.write",
		"delivery.read", "delivery.write", "delivery.approve", "delivery.execute",
		"insights.read", "insights.write", "insights.confirm",
		"knowledge.document_vision.reconcile",
		// 只有 admin 能改模型服务配置：那是一份组织级凭据，改错了全组织都出不了片。
		"provider.configuration.write",
	}
	return contract.ScopesFromStrings(values)
}

func memberScopes() []contract.Scope {
	values := []string{
		"identity.profile.write", "organization.read", "organization.members.read", "project.members.read",
		"project.read", "project.write", "assets.read", "assets.write",
		"provider.read", "provider.generate", "provider.job.create", "provider.text.generate",
		"strategy.read", "strategy.write", "strategy.confirm", "strategy.review",
		"strategy.approve", "strategy.package.read", "creative.read", "creative.write",
		"delivery.read", "delivery.write", "delivery.approve", "delivery.execute",
		// **member 没有 insights.confirm**，这是有意的。
		//
		// 洞察这三个权限的整个意义在于「提特征的人和认结论的人可以不是同一个人」
		// （03 §11.1、AM-006）：确认动作会把一条结论变成可被下一轮引用的东西，
		// 改判定阈值更是一改改全组织。以前 member 拿全三档，等于组织里每个人都能
		// 单方面把机器说的变成「我们认的」——设置页上那句「分开授予」于是成了空话。
		//
		// 要让某个人能确认，把他的组织角色提到 admin。按人单授权还没有对象模型
		// （scope 现在只从 organization_memberships.role 推），那是后面的事。
		"insights.read", "insights.write",
	}
	return contract.ScopesFromStrings(values)
}

func auditorScopes() []contract.Scope {
	return contract.ScopesFromStrings([]string{
		"identity.profile.write", "organization.read", "organization.members.read", "project.members.read",
		"project.read", "assets.read", "provider.read", "strategy.read",
		"strategy.package.read", "creative.read", "delivery.read", "insights.read",
	})
}

func ScopesForOrganizationRole(role string) ([]contract.Scope, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "admin":
		return adminScopes(), nil
	case "member":
		return memberScopes(), nil
	case "auditor":
		return auditorScopes(), nil
	default:
		return nil, ErrActorInactive
	}
}

func duplicateKey(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func hashPassword(password string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordHashIterations, 32)
	encoded := fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		passwordHashIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	)
	return []byte(encoded), nil
}

func verifyPassword(encoded []byte, password string) bool {
	parts := strings.Split(string(encoded), "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil ||
		iterations < 100000 || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func burnUnknownCredential(password string) {
	salt := []byte("cookies-auth-dummy")
	expected := make([]byte, 32)
	actual := pbkdf2SHA256([]byte(password), salt, passwordHashIterations, len(expected))
	_ = subtle.ConstantTimeCompare(actual, expected)
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	result := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		value := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range value {
				value[j] ^= u[j]
			}
		}
		result = append(result, value...)
	}
	return result[:keyLength]
}
