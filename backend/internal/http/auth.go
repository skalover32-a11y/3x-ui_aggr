package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if req.Username != h.AdminUser || req.Password != h.AdminPass {
		respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid credentials")
		return
	}
	now := time.Now()
	expiry := h.JWTExpiry
	if expiry == 0 {
		expiry = 24 * time.Hour
	}
	claims := jwt.MapClaims{
		"sub":  "admin",
		"role": "admin",
		"iat":  now.Unix(),
		"exp":  now.Add(expiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(h.JWTSecret)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "TOKEN_SIGN", "failed to sign token")
		return
	}
	respondStatus(c, http.StatusOK, loginResponse{Token: signed})
}
