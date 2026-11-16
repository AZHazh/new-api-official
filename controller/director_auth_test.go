package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectorWebBinding(t *testing.T) {
	t.Setenv("DIRECTOR_AUTH_CLIENT_ID", "director-web")
	t.Setenv("DIRECTOR_AUTH_CLIENT_SECRET", strings.Repeat("s", 40))
	t.Setenv("DIRECTOR_AUTH_REDIRECT_URI", "https://director.example/auth/callback")
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	require.NoError(t, validateDirectorWebIssue("director-web", "https://director.example/auth/callback", challenge, "S256"))
	require.NoError(t, validateDirectorWebExchange("director-web", "https://director.example/auth/callback", challenge, verifier, "director-web", strings.Repeat("s", 40)))
	for _, tc := range []struct{ name, client, redirect, proof, authID, authSecret string }{
		{"wrong client", "other", "https://director.example/auth/callback", verifier, "director-web", strings.Repeat("s", 40)},
		{"unregistered callback", "director-web", "https://evil.example/auth/callback", verifier, "director-web", strings.Repeat("s", 40)},
		{"wrong proof", "director-web", "https://director.example/auth/callback", strings.Repeat("b", 43), "director-web", strings.Repeat("s", 40)},
		{"wrong secret", "director-web", "https://director.example/auth/callback", verifier, "director-web", "incorrect"},
		{"wrong basic ID", "director-web", "https://director.example/auth/callback", verifier, "other", strings.Repeat("s", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Error(t, validateDirectorWebExchange(tc.client, tc.redirect, challenge, tc.proof, tc.authID, tc.authSecret))
		})
	}
	assert.Error(t, validateDirectorWebIssue("director-web", "https://director.example/auth/callback", challenge, "plain"))
}

func TestDirectorWebConfigFailsClosed(t *testing.T) {
	t.Setenv("DIRECTOR_AUTH_CLIENT_ID", "director-web")
	t.Setenv("DIRECTOR_AUTH_CLIENT_SECRET", strings.Repeat("s", 40))
	for _, callback := range []string{"http://public.example/auth/callback", "https://director.example/auth/callback?next=evil", "https://user:password@director.example/auth/callback", "https://director.example/wrong"} {
		t.Setenv("DIRECTOR_AUTH_REDIRECT_URI", callback)
		_, err := loadDirectorWebConfig()
		assert.Error(t, err, callback)
	}
	t.Setenv("DIRECTOR_AUTH_REDIRECT_URI", "https://director.example/auth/callback")
	t.Setenv("DIRECTOR_AUTH_CLIENT_SECRET", "short")
	_, err := loadDirectorWebConfig()
	assert.Error(t, err)
}
