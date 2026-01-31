package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/services/agentauth"
)

func RequireOrgRole(dbConn *gorm.DB, adminUser string, minRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		orgIDStr := strings.TrimSpace(c.Param("orgId"))
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ORG", "message": "invalid org id"}})
			c.Abort()
			return
		}
		actor := c.GetString("actor")
		if actor == "" {
			respondUnauthorized(c)
			return
		}
		if strings.EqualFold(actor, adminUser) {
			c.Set("org_id", orgID.String())
			c.Set("org_role", "owner")
			c.Next()
			return
		}
		var user db.User
		if err := dbConn.WithContext(c.Request.Context()).Where("lower(username) = lower(?)", actor).First(&user).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "forbidden"}})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "DB_READ", "message": "failed to read user"}})
			c.Abort()
			return
		}
		var member db.OrganizationMember
		if err := dbConn.WithContext(c.Request.Context()).First(&member, "org_id = ? AND user_id = ?", orgID, user.ID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "forbidden"}})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "DB_READ", "message": "failed to read membership"}})
			c.Abort()
			return
		}
		if !agentauth.HasMinRole(member.Role, minRole) {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "forbidden"}})
			c.Abort()
			return
		}
		c.Set("org_id", orgID.String())
		c.Set("org_role", member.Role)
		c.Next()
	}
}
