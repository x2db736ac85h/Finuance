package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/x2db736ac85h/finuance/backend/internal/auth"
	"github.com/x2db736ac85h/finuance/backend/internal/instance"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
)

// TestAccessTokens: a token resolves to its user and scope until it is revoked or expires; at most
// 25 live ones; old ones are cleaned up 30 days after they ended (ADR 0036).
func TestAccessTokens(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	u, _ := signup(t, s)

	tok, secret, err := s.CreateToken(ctx, u.ID, " laptop ", auth.ScopeRead, 30)
	if err != nil || tok.Name != "laptop" || tok.Prefix != secret[:12] || !auth.IsAccessToken(secret) ||
		!tok.ExpiresAt.Equal(clk.Now().Add(30*24*time.Hour)) {
		t.Fatalf("created = %+v %q %v", tok, secret, err)
	}
	id, err := s.ResolveToken(ctx, secret)
	if err != nil || id.User.ID != u.ID || id.Token == nil || id.Token.Scope != auth.ScopeRead || id.Token.ID != tok.ID {
		t.Fatalf("resolved = %+v %v", id, err)
	}
	if list, _ := s.Tokens(ctx, u.ID); len(list) != 1 || list[0].LastUsedAt == nil {
		t.Errorf("list = %+v (last use recorded)", list)
	}
	for _, bad := range []string{secret + "x", "fin_", "sk-other", ""} {
		if _, err := s.ResolveToken(ctx, bad); !errors.Is(err, platform.ErrUnauthenticated) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	var ve *platform.ValidationError
	if _, _, err := s.CreateToken(ctx, u.ID, "", "admin", 0); !errors.As(err, &ve) {
		t.Errorf("invalid: %v", err)
	}

	// another user can't revoke it
	other, _, err := s.Signup(ctx, auth.SignupInput{Email: "other-" + email(t), Password: pw}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeToken(ctx, other.ID, tok.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("other user revoked it: %v", err)
	}
	if list, _ := s.Tokens(ctx, other.ID); len(list) != 0 {
		t.Errorf("other user lists %+v", list)
	}

	// at most 25 live tokens
	for i := 1; i < auth.MaxTokens; i++ {
		if _, _, err := s.CreateToken(ctx, u.ID, "t", auth.ScopeWrite, 1); err != nil {
			t.Fatal(err)
		}
	}
	var le *instance.LimitError
	if _, _, err := s.CreateToken(ctx, u.ID, "one too many", auth.ScopeRead, 1); !errors.As(err, &le) || le.Limit != "accessTokens" {
		t.Errorf("26th token: %v", err)
	}

	// expiry, revocation and cleanup
	clk.Add(2 * 24 * time.Hour)
	if _, _, err := s.CreateToken(ctx, u.ID, "after the day ones ended", auth.ScopeRead, 1); err != nil {
		t.Errorf("expired tokens still count: %v", err)
	}
	if _, err := s.RevokeToken(ctx, u.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveToken(ctx, secret); !errors.Is(err, platform.ErrUnauthenticated) {
		t.Errorf("revoked token: %v", err)
	}
	clk.Add(32 * 24 * time.Hour) // more than 30 days after the last one ended
	if n, err := s.CleanupAccessTokens(ctx); err != nil || n != 26 {
		t.Errorf("cleaned up %d, %v (24 expired, the revoked one, the later day one)", n, err)
	}
}
