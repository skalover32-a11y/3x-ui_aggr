package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/http/middleware"
)

type userResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type userCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type userUpdateRequest struct {
	Password *string `json:"password"`
	Role     *string `json:"role"`
}

func (h *Handler) ListUsers(c *gin.Context) {
	var users []db.User
	if err := h.DB.WithContext(c.Request.Context()).Order("created_at desc").Find(&users).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list users")
		return
	}
	resp := make([]userResponse, 0, len(users))
	for _, user := range users {
		resp = append(resp, toUserResponse(user))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateUser(c *gin.Context) {
	var req userCreateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		respondError(c, http.StatusBadRequest, "USER_NAME", "username required")
		return
	}
	if req.Password == "" {
		respondError(c, http.StatusBadRequest, "USER_PASSWORD", "password required")
		return
	}
	role := normalizeRole(req.Role)
	if role == "" {
		respondError(c, http.StatusBadRequest, "USER_ROLE", "invalid role")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "USER_PASSWORD", "failed to hash password")
		return
	}
	user := db.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		respondError(c, http.StatusBadRequest, "USER_CREATE", "failed to create user")
		return
	}
	h.auditEvent(c, nil, "USER_CREATE", "ok", nil, gin.H{"username": username, "role": role}, nil)
	respondStatus(c, http.StatusCreated, toUserResponse(user))
}

func (h *Handler) UpdateUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "USER_ID", "invalid user id")
		return
	}
	var user db.User
	if err := h.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	var req userUpdateRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if req.Role != nil {
		role := normalizeRole(*req.Role)
		if role == "" {
			respondError(c, http.StatusBadRequest, "USER_ROLE", "invalid role")
			return
		}
		user.Role = role
	}
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "USER_PASSWORD", "failed to hash password")
			return
		}
		user.PasswordHash = string(hash)
	}
	user.UpdatedAt = time.Now()
	if err := h.DB.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "USER_UPDATE", "failed to update user")
		return
	}
	h.auditEvent(c, nil, "USER_UPDATE", "ok", nil, gin.H{"username": user.Username, "role": user.Role}, nil)
	respondStatus(c, http.StatusOK, toUserResponse(user))
}

func (h *Handler) DeleteUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "USER_ID", "invalid user id")
		return
	}
	var user db.User
	if err := h.DB.WithContext(c.Request.Context()).First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "NOT_FOUND", "user not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "DB_READ", "failed to read user")
		return
	}
	if err := h.DB.WithContext(c.Request.Context()).Delete(&db.User{}, "id = ?", userID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "USER_DELETE", "failed to delete user")
		return
	}
	h.auditEvent(c, nil, "USER_DELETE", "ok", nil, gin.H{"username": user.Username}, nil)
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case middleware.RoleAdmin:
		return middleware.RoleAdmin
	case middleware.RoleOperator:
		return middleware.RoleOperator
	case middleware.RoleViewer:
		return middleware.RoleViewer
	default:
		return ""
	}
}

func toUserResponse(user db.User) userResponse {
	return userResponse{
		ID:        user.ID.String(),
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}
