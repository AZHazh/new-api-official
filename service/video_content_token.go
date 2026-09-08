package service

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	VideoContentCookieName = "new_api_video_content"
	VideoContentTokenTTL   = 2 * time.Hour
	videoContentTokenUse   = "video_content"
)

// IssueVideoContentToken creates a read-only, purpose-bound browser grant. It
// is accepted only by the video content proxy and still requires a live login
// session when used.
func IssueVideoContentToken(identity AuthIdentity) (string, int64, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || identity.UserAuthVersion <= 0 || identity.SessionVersion <= 0 {
		return "", 0, ErrAuthTokenInvalid
	}
	now := time.Now()
	expiresAt := now.Add(VideoContentTokenTTL)
	claims := authClaims{
		TokenUse:        videoContentTokenUse,
		SessionID:       identity.SessionID,
		UserAuthVersion: identity.UserAuthVersion,
		SessionVersion:  identity.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authTokenIssuer,
			Subject:   strconv.Itoa(identity.UserID),
			Audience:  jwt.ClaimStrings{authTokenAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(authSigningKey(videoContentTokenUse))
	return signed, expiresAt.Unix(), err
}

func ParseVideoContentToken(raw string) (AuthIdentity, error) {
	claims, err := parseAuthClaims(raw, videoContentTokenUse, authSigningKey(videoContentTokenUse))
	if err != nil {
		return AuthIdentity{}, err
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID <= 0 || claims.SessionID == "" || claims.UserAuthVersion <= 0 || claims.SessionVersion <= 0 {
		return AuthIdentity{}, ErrAuthTokenInvalid
	}
	return AuthIdentity{
		UserID:          userID,
		SessionID:       claims.SessionID,
		UserAuthVersion: claims.UserAuthVersion,
		SessionVersion:  claims.SessionVersion,
	}, nil
}

func WriteVideoContentCookie(c *gin.Context, identity AuthIdentity) error {
	raw, expiresAt, err := IssueVideoContentToken(identity)
	if err != nil {
		return err
	}
	expires := time.Unix(expiresAt, 0)
	maxAge := int(time.Until(expires) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     VideoContentCookieName,
		Value:    raw,
		Path:     "/v1/videos",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}
