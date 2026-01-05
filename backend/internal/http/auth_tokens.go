package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

const refreshCookieName = "agg_refresh"

func (h *Handler) issueAccessToken(username, role string) (string, error) {
	now := time.Now()
	expiry := h.JWTExpiry
	if expiry == 0 {
		expiry = 24 * time.Hour
	}
	claims := jwt.MapClaims{
		"sub":  username,
		"user": username,
		"role": role,
		"iat":  now.Unix(),
		"exp":  now.Add(expiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.JWTSecret)
}

func (h *Handler) resolveRole(ctx *gin.Context, username string) (string, error) {
	if username == h.AdminUser {
		return middleware.RoleAdmin, nil
	}
	var user db.User
	err := h.DB.WithContext(ctx.Request.Context()).Where("lower(username) = lower(?)", username).First(&user).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", err
		}
		return "", err
	}
	return user.Role, nil
}

func (h *Handler) issueRefreshToken(ctx *gin.Context, username string) (string, *db.RefreshToken, error) {
	if h.RefreshTTL <= 0 {
		h.RefreshTTL = 720 * time.Hour
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", nil, err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf[:])
	hash := hashRefreshToken(raw)
	now := time.Now()
	ua := strings.TrimSpace(ctx.Request.UserAgent())
	ip := strings.TrimSpace(ctx.ClientIP())
	row := db.RefreshToken{
		UserID:    username,
		TokenHash: hash,
		CreatedAt: now,
		ExpiresAt: now.Add(h.RefreshTTL),
	}
	if ua != "" {
		row.UserAgent = &ua
	}
	if ip != "" {
		row.IP = &ip
	}
	if err := h.DB.WithContext(ctx.Request.Context()).Create(&row).Error; err != nil {
		return "", nil, err
	}
	return raw, &row, nil
}

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) setRefreshCookie(c *gin.Context, token string, ttl time.Duration) {
	if ttl <= 0 {
		ttl = 720 * time.Hour
	}
	maxAge := int(ttl.Seconds())
	secure := true
	host := strings.ToLower(c.Request.Host)
	if strings.Contains(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		secure = false
	}
	if proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))); proto != "" {
		secure = proto == "https"
	}
	httpOnly := true
	c.SetCookie(refreshCookieName, token, maxAge, "/api", "", secure, httpOnly)
}

func (h *Handler) clearRefreshCookie(c *gin.Context) {
	secure := true
	host := strings.ToLower(c.Request.Host)
	if strings.Contains(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") {
		secure = false
	}
	if proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))); proto != "" {
		secure = proto == "https"
	}
	c.SetCookie(refreshCookieName, "", -1, "/api", "", secure, true)
}

func (h *Handler) readRefreshCookie(c *gin.Context) string {
	val, err := c.Cookie(refreshCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}

func requireXHR(c *gin.Context) bool {
	val := strings.TrimSpace(c.GetHeader("X-Requested-With"))
	return strings.EqualFold(val, "XMLHttpRequest")
}
