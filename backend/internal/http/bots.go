package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
)

type botRequest struct {
	NodeID          *string `json:"node_id"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	DockerContainer *string `json:"docker_container"`
	SystemdUnit     *string `json:"systemd_unit"`
	HealthURL       *string `json:"health_url"`
	HealthPath      *string `json:"health_path"`
	ExpectedStatus  []int   `json:"expected_status"`
	IsEnabled       *bool   `json:"is_enabled"`
}

type botResponse struct {
	ID              string    `json:"id"`
	NodeID          string    `json:"node_id"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	DockerContainer *string   `json:"docker_container"`
	SystemdUnit     *string   `json:"systemd_unit"`
	HealthURL       *string   `json:"health_url"`
	HealthPath      *string   `json:"health_path"`
	ExpectedStatus  []int     `json:"expected_status"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func normalizeBotKind(raw string) (string, error) {
	kind := strings.ToUpper(strings.TrimSpace(raw))
	switch kind {
	case "DOCKER", "SYSTEMD", "HTTP":
		return kind, nil
	default:
		return "", errInvalid("invalid bot kind")
	}
}

func (h *Handler) getBot(ctx *gin.Context, idStr string) (*db.Bot, error) {
	botID, err := uuid.Parse(idStr)
	if err != nil {
		return nil, err
	}
	var bot db.Bot
	if err := h.DB.WithContext(ctx.Request.Context()).First(&bot, "id = ?", botID).Error; err != nil {
		return nil, err
	}
	return &bot, nil
}

func toBotResponse(bot *db.Bot) botResponse {
	return botResponse{
		ID:              bot.ID.String(),
		NodeID:          bot.NodeID.String(),
		Name:            bot.Name,
		Kind:            bot.Kind,
		DockerContainer: bot.DockerContainer,
		SystemdUnit:     bot.SystemdUnit,
		HealthURL:       bot.HealthURL,
		HealthPath:      bot.HealthPath,
		ExpectedStatus:  intArray(bot.ExpectedStatus),
		IsEnabled:       bot.IsEnabled,
		CreatedAt:       bot.CreatedAt,
		UpdatedAt:       bot.UpdatedAt,
	}
}

func (h *Handler) buildBotFromRequest(nodeID uuid.UUID, req *botRequest) (*db.Bot, error) {
	enabled := true
	if req.IsEnabled != nil {
		enabled = *req.IsEnabled
	}
	expected := int64Array(req.ExpectedStatus)
	if len(expected) == 0 {
		expected = pq.Int64Array{200}
	}
	path := trimPtr(req.HealthPath)
	if path == nil {
		defaultPath := "/"
		path = &defaultPath
	}
	return &db.Bot{
		NodeID:          nodeID,
		Name:            strings.TrimSpace(req.Name),
		Kind:            strings.ToUpper(strings.TrimSpace(req.Kind)),
		DockerContainer: trimPtr(req.DockerContainer),
		SystemdUnit:     trimPtr(req.SystemdUnit),
		HealthURL:       trimPtr(req.HealthURL),
		HealthPath:      path,
		ExpectedStatus:  expected,
		IsEnabled:       enabled,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}, nil
}

func (h *Handler) createDefaultBotCheck(ctx context.Context, tx *gorm.DB, bot *db.Bot) error {
	var count int64
	if err := tx.WithContext(ctx).Model(&db.Check{}).
		Where("target_type = ? AND target_id = ?", "bot", bot.ID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	checkType := strings.ToUpper(strings.TrimSpace(bot.Kind))
	if checkType == "" {
		checkType = "HTTP"
	}
	row := db.Check{
		TargetType:    "bot",
		TargetID:      bot.ID,
		Type:          checkType,
		IntervalSec:   30,
		TimeoutMS:     3000,
		Retries:       1,
		Enabled:       true,
		SeverityRules: datatypes.JSON([]byte("{}")),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return tx.WithContext(ctx).Create(&row).Error
}

func (h *Handler) validateBotRequest(kind string, req *botRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return errInvalid("name required")
	}
	switch kind {
	case "DOCKER":
		if strings.TrimSpace(stringValue(req.DockerContainer)) == "" {
			return errInvalid("docker_container required")
		}
	case "SYSTEMD":
		if strings.TrimSpace(stringValue(req.SystemdUnit)) == "" {
			return errInvalid("systemd_unit required")
		}
	}
	return nil
}

func (h *Handler) ListBots(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var rows []db.Bot
	if err := h.DB.WithContext(c.Request.Context()).Where("node_id = ?", node.ID).Find(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list bots")
		return
	}
	resp := make([]botResponse, 0, len(rows))
	for i := range rows {
		resp = append(resp, toBotResponse(&rows[i]))
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) CreateBot(c *gin.Context) {
	node, err := h.getNodeForActor(c, c.Param("id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "node not found")
		return
	}
	var req botRequest
	if !parseJSONBody(c, &req) {
		return
	}
	kind, err := normalizeBotKind(req.Kind)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_KIND", "invalid bot kind")
		return
	}
	if err := h.validateBotRequest(kind, &req); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_BOT", err.Error())
		return
	}
	req.Kind = kind
	bot, err := h.buildBotFromRequest(node.ID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_BOT", "invalid bot request")
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Create(bot).Error; err != nil {
			return err
		}
		return h.createDefaultBotCheck(ctx, tx, bot)
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_CREATE", "failed to create bot")
		return
	}
	respondStatus(c, http.StatusCreated, toBotResponse(bot))
}

func (h *Handler) GetBot(c *gin.Context) {
	bot, err := h.getBotForActor(c, c.Param("bot_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	respondStatus(c, http.StatusOK, toBotResponse(bot))
}

func (h *Handler) UpdateBot(c *gin.Context) {
	bot, err := h.getBotForActor(c, c.Param("bot_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	var req botRequest
	if !parseJSONBody(c, &req) {
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		bot.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.Kind) != "" {
		kind, err := normalizeBotKind(req.Kind)
		if err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_KIND", "invalid bot kind")
			return
		}
		bot.Kind = kind
	}
	if req.DockerContainer != nil {
		bot.DockerContainer = trimPtr(req.DockerContainer)
	}
	if req.SystemdUnit != nil {
		bot.SystemdUnit = trimPtr(req.SystemdUnit)
	}
	if req.HealthURL != nil {
		bot.HealthURL = trimPtr(req.HealthURL)
	}
	if req.HealthPath != nil {
		path := strings.TrimSpace(*req.HealthPath)
		if path == "" {
			path = "/"
		}
		bot.HealthPath = &path
	}
	if req.ExpectedStatus != nil && len(req.ExpectedStatus) > 0 {
		bot.ExpectedStatus = int64Array(req.ExpectedStatus)
	}
	if req.IsEnabled != nil {
		bot.IsEnabled = *req.IsEnabled
	}
	if req.NodeID != nil {
		nodeRaw := strings.TrimSpace(*req.NodeID)
		if nodeRaw == "" {
			respondError(c, http.StatusBadRequest, "INVALID_NODE", "node_id required")
			return
		}
		targetNode, err := h.getNodeForActor(c, nodeRaw)
		if err != nil {
			respondError(c, http.StatusBadRequest, "INVALID_NODE", "node not found")
			return
		}
		bot.NodeID = targetNode.ID
	}
	if err := h.validateBotRequest(strings.ToUpper(strings.TrimSpace(bot.Kind)), &botRequest{
		Name:            bot.Name,
		Kind:            bot.Kind,
		DockerContainer: bot.DockerContainer,
		SystemdUnit:     bot.SystemdUnit,
	}); err != nil {
		respondError(c, http.StatusBadRequest, "INVALID_BOT", err.Error())
		return
	}
	bot.UpdatedAt = time.Now()
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Save(bot).Error; err != nil {
			return err
		}
		if strings.TrimSpace(bot.Kind) != "" {
			if err := tx.WithContext(ctx).Model(&db.Check{}).
				Where("target_type = ? AND target_id = ?", "bot", bot.ID).
				Update("type", bot.Kind).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_UPDATE", "failed to update bot")
		return
	}
	respondStatus(c, http.StatusOK, toBotResponse(bot))
}

func (h *Handler) DeleteBot(c *gin.Context) {
	bot, err := h.getBotForActor(c, c.Param("bot_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	ctx := c.Request.Context()
	if err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Exec(`
			DELETE FROM check_results
			WHERE check_id IN (
				SELECT id FROM checks WHERE target_type = 'bot' AND target_id = ?
			)
		`, bot.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.Check{}, "target_type = ? AND target_id = ?", "bot", bot.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.AlertState{}, "bot_id = ?", bot.ID).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&db.Bot{}, "id = ?", bot.ID).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "DB_DELETE", "failed to delete bot")
		return
	}
	respondStatus(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) RunBotCheck(c *gin.Context) {
	bot, err := h.getBotForActor(c, c.Param("bot_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	if h.Checks == nil {
		respondError(c, http.StatusServiceUnavailable, "CHECKS_DISABLED", "checks worker not configured")
		return
	}
	result, err := h.Checks.RunNowBot(c.Request.Context(), bot.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "RUN_FAILED", "failed to run check")
		return
	}
	resp := checkResultResponse{
		ID:        result.ID.String(),
		CheckID:   result.CheckID.String(),
		TS:        result.TS,
		Status:    result.Status,
		Metrics:   json.RawMessage(result.Metrics),
		Error:     result.Error,
		LatencyMS: result.LatencyMS,
	}
	respondStatus(c, http.StatusOK, resp)
}

func (h *Handler) ListBotResults(c *gin.Context) {
	bot, err := h.getBotForActor(c, c.Param("bot_id"))
	if err != nil {
		respondError(c, http.StatusNotFound, "NOT_FOUND", "bot not found")
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			limit = val
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	minutes := 0
	if raw := c.Query("minutes"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil {
			minutes = val
		}
	}
	var since time.Time
	if minutes > 0 {
		since = time.Now().Add(-time.Duration(minutes) * time.Minute)
	}
	var rows []db.CheckResult
	query := h.DB.WithContext(c.Request.Context()).
		Table("check_results cr").
		Select("cr.id, cr.check_id, cr.ts, cr.status, cr.metrics, cr.error, cr.latency_ms").
		Joins("JOIN checks c ON c.id = cr.check_id").
		Where("c.target_type = 'bot' AND c.target_id = ?", bot.ID)
	if !since.IsZero() {
		query = query.Where("cr.ts >= ?", since)
	}
	if err := query.Order("cr.ts desc").Limit(limit).Scan(&rows).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "DB_LIST", "failed to list bot results")
		return
	}
	resp := make([]checkResultResponse, 0, len(rows))
	for i := range rows {
		row := rows[i]
		resp = append(resp, checkResultResponse{
			ID:        row.ID.String(),
			CheckID:   row.CheckID.String(),
			TS:        row.TS,
			Status:    row.Status,
			Metrics:   json.RawMessage(row.Metrics),
			Error:     row.Error,
			LatencyMS: row.LatencyMS,
		})
	}
	respondStatus(c, http.StatusOK, resp)
}

type invalidError struct{ msg string }

func (e invalidError) Error() string { return e.msg }

func errInvalid(msg string) error { return invalidError{msg: msg} }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
