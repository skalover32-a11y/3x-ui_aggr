package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(c *gin.Context) {
		role := c.GetString("role")
		if role == "" {
			respondUnauthorized(c)
			return
		}
		if !allowed[role] {
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"code":    "FORBIDDEN",
					"message": "forbidden",
				},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
