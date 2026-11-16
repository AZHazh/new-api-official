package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// Validate the registered destination before displaying consent or offering cancel.
// No account, authorization code or client secret is returned by this endpoint.
func DirectorWebAuthorizationInfo(c *gin.Context) {
	if err := validateDirectorWebIssue(c.Query("client_id"), c.Query("redirect_uri"), c.Query("code_challenge"), c.Query("code_challenge_method")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"client_id":    c.Query("client_id"),
		"redirect_uri": c.Query("redirect_uri"),
	})
}
