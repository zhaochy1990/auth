package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zhaochy1990/auth-service/internal/apperror"
	"github.com/zhaochy1990/auth-service/internal/middleware"
)

// publicKeyResponse is the JSON body exposing the service's RSA public key so
// clients can verify JWTs offline. The key is PEM-encoded, PKIX format.
type publicKeyResponse struct {
	PublicKey string `json:"publickey"`
}

// GetPublicKey returns the Auth service's RSA public key. The endpoint is
// public — the key is by definition not secret — and requires no client id or
// Bearer token.
//
// @Summary		Get the service public key
// @Description	Returns the RSA public key (PEM, PKIX format) used to verify JWTs.
// @Tags			system
// @Produce		json
// @Success		200	{object}	publicKeyResponse
// @Failure		500	{object}	ErrorResponse
// @Router			/api/system/public-key [get]
func (h *Handler) GetPublicKey(c *gin.Context) {
	key := h.JWT.PublicKeyPEM()
	if key == "" {
		middleware.RespondError(c, apperror.Internal())
		return
	}
	c.JSON(http.StatusOK, publicKeyResponse{PublicKey: key})
}
