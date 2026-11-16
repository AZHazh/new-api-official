package controller

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"regexp"
)

var directorClientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var directorChallengePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{43}$`)
var directorVerifierPattern = regexp.MustCompile(`^[a-zA-Z0-9._~-]{43,128}$`)

type directorWebConfig struct {
	ClientID    string
	Secret      string
	RedirectURI string
}

func loadDirectorWebConfig() (directorWebConfig, error) {
	cfg := directorWebConfig{
		ClientID:    os.Getenv("DIRECTOR_AUTH_CLIENT_ID"),
		Secret:      os.Getenv("DIRECTOR_AUTH_CLIENT_SECRET"),
		RedirectURI: os.Getenv("DIRECTOR_AUTH_REDIRECT_URI"),
	}
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || !directorClientIDPattern.MatchString(cfg.ClientID) || len(cfg.Secret) < 32 {
		return cfg, errors.New("cloud director authorization is not configured")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Hostname() == "" || (u.Scheme != "https" && !(u.Scheme == "http" && loopback)) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/auth/callback" {
		return cfg, errors.New("cloud director callback configuration is invalid")
	}
	return cfg, nil
}

func validateDirectorWebIssue(clientID, redirectURI, challenge, method string) error {
	cfg, err := loadDirectorWebConfig()
	if err != nil {
		return err
	}
	if clientID != cfg.ClientID || redirectURI != cfg.RedirectURI || method != "S256" || !directorChallengePattern.MatchString(challenge) {
		return errors.New("invalid cloud director authorization request")
	}
	return nil
}

func validateDirectorWebExchange(clientID, redirectURI, challenge, verifier, authID, authSecret string) error {
	if err := validateDirectorWebIssue(clientID, redirectURI, challenge, "S256"); err != nil {
		return err
	}
	cfg, err := loadDirectorWebConfig()
	if err != nil {
		return err
	}
	if authID != cfg.ClientID || subtle.ConstantTimeCompare([]byte(authSecret), []byte(cfg.Secret)) != 1 || !directorVerifierPattern.MatchString(verifier) {
		return errors.New("invalid cloud director client credentials")
	}
	digest := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) != 1 {
		return errors.New("invalid cloud director proof key")
	}
	return nil
}
