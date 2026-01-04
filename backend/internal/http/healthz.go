package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Healthz(c *gin.Context) {
	dbOK := false
	if h != nil && h.DB != nil {
		if err := h.DB.WithContext(c.Request.Context()).Exec("select 1").Error; err == nil {
			dbOK = true
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"db": dbOK,
	})
}
