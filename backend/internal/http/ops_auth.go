package httpapi

import (
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"agr_3x_ui/internal/http/middleware"
)

func (h *Handler) OpsReadAuth() gin.HandlerFunc {
	allowedRoles := map[string]bool{
		middleware.RoleAdmin:    true,
		middleware.RoleOperator: true,
		middleware.RoleViewer:   true,
	}
	return func(c *gin.Context) {
		if h.masterKeyEnabled() {
			if ok, reason := h.checkMasterAuth(c); ok {
				c.Set("actor", "master")
				c.Set("role", middleware.RoleAdmin)
				c.Set("master_auth", true)
				log.Printf("ops master auth ok ip=%s", c.ClientIP())
				c.Next()
				return
			} else if reason != "" {
				respondError(c, http.StatusForbidden, "FORBIDDEN", reason)
				return
			}
		}
		role, actor, err := h.parseJWTAuth(c)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		if !allowedRoles[role] {
			respondError(c, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		c.Set("role", role)
		if actor != "" {
			c.Set("actor", actor)
		}
		c.Next()
	}
}

func (h *Handler) masterKeyEnabled() bool {
	return strings.TrimSpace(h.MasterKey) != ""
}

func (h *Handler) checkMasterAuth(c *gin.Context) (bool, string) {
	key := strings.TrimSpace(c.GetHeader("X-Agg-Master-Key"))
	if key == "" {
		return false, ""
	}
	if key != strings.TrimSpace(h.MasterKey) {
		return false, "invalid master key"
	}
	if !h.allowMasterIP(c.ClientIP()) {
		return false, "master auth not allowed from this IP"
	}
	return true, ""
}

func (h *Handler) allowMasterIP(ipStr string) bool {
	if ipStr == "" {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	allowList := strings.TrimSpace(h.AllowCIDR)
	if allowList == "" {
		return false
	}
	for _, part := range strings.Split(allowList, ",") {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		_, netBlock, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if netBlock.Contains(ip) {
			return true
		}
	}
	return false
}

func (h *Handler) parseJWTAuth(c *gin.Context) (string, string, error) {
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", "", jwt.ErrTokenSignatureInvalid
	}
	tokenStr := strings.TrimPrefix(auth, "Bearer ")
	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrTokenSignatureInvalid
		}
		return h.JWTSecret, nil
	})
	if err != nil || !token.Valid {
		return "", "", err
	}
	actor := claims.User
	if actor == "" {
		actor = claims.Subject
	}
	if actor == "" {
		actor = "admin"
	}
	return claims.Role, actor, nil
}
