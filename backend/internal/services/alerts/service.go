package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

const (
	dedupTTL              = 5 * time.Minute
	defaultAckMuteMinutes = 24 * 60
	defaultButtonMuteMins = 60
	maxPolicyMinutes      = 30 * 24 * 60
)

type Policy struct {
	CPULoadThreshold float64
	MemPercentHigh   float64
	DiskFreeLow      float64
	OfflineDelay     time.Duration
}

func defaultPolicy() Policy {
	return Policy{
		CPULoadThreshold: 2.0,
		MemPercentHigh:   90.0,
		DiskFreeLow:      10.0,
		OfflineDelay:     5 * time.Minute,
	}
}

type Option func(*Service)

func WithPolicy(policy Policy) Option {
	return func(s *Service) {
		if s == nil {
			return
		}
		if policy.CPULoadThreshold > 0 {
			s.policy.CPULoadThreshold = policy.CPULoadThreshold
		}
		if policy.MemPercentHigh > 0 {
			s.policy.MemPercentHigh = policy.MemPercentHigh
		}
		if policy.DiskFreeLow > 0 {
			s.policy.DiskFreeLow = policy.DiskFreeLow
		}
		if policy.OfflineDelay >= 0 {
			s.policy.OfflineDelay = policy.OfflineDelay
		}
	}
}

type Service struct {
	db            *gorm.DB
	enc           *security.Encryptor
	publicBaseURL string
	client        *telegramClient
	dedup         *Deduper
	policy        Policy
	mu            sync.Mutex
	mutedUntil    map[string]time.Time
}

type Settings struct {
	BotToken        string
	AdminChatIDs    []string
	AlertConnection bool
	AlertCPU        bool
	AlertMemory     bool
	AlertDisk       bool
	AckMuteMinutes  int
	MuteMinutes     int
}

type alertMessageID struct {
	ChatID    string `json:"chat_id"`
	MessageID int    `json:"message_id"`
}

type SendResult struct {
	ChatID string `json:"chat_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

func New(dbConn *gorm.DB, enc *security.Encryptor, publicBaseURL string, opts ...Option) *Service {
	svc := &Service{
		db:            dbConn,
		enc:           enc,
		publicBaseURL: strings.TrimSpace(publicBaseURL),
		client:        newTelegramClient(),
		dedup:         NewDeduper(dedupTTL),
		policy:        defaultPolicy(),
		mutedUntil:    map[string]time.Time{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

func (s *Service) LoadSettings(ctx context.Context) (*Settings, error) {
	if s == nil || s.db == nil || s.enc == nil {
		return nil, nil
	}
	var row db.TelegramSettings
	err := s.db.WithContext(ctx).Order("created_at desc").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return s.settingsFromRow(&row)
}

func (s *Service) settingsFromRow(row *db.TelegramSettings) (*Settings, error) {
	if s == nil || s.enc == nil || row == nil {
		return nil, nil
	}
	token, err := s.enc.DecryptString(row.BotTokenEnc)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.TrimSpace(row.AdminChatID) == "" {
		return nil, nil
	}
	adminIDs := splitChatIDs(row.AdminChatID)
	if len(adminIDs) == 0 {
		return nil, nil
	}
	ackMute, mute := normalizePolicyMinutes(row.AckMuteMinutes, row.MuteMinutes)
	return &Settings{
		BotToken:        token,
		AdminChatIDs:    adminIDs,
		AlertConnection: row.AlertConnection,
		AlertCPU:        row.AlertCPU,
		AlertMemory:     row.AlertMemory,
		AlertDisk:       row.AlertDisk,
		AckMuteMinutes:  ackMute,
		MuteMinutes:     mute,
	}, nil
}

func (s *Service) NotifyConnection(ctx context.Context, settings *Settings, node *db.Node, panelOK bool, panelErr *string, panelCode *string, sshOK bool, sshErr *string) {
	if settings == nil || !settings.AlertConnection || node == nil {
		return
	}
	now := time.Now()

	sshAlert := Alert{
		Type:       AlertConnection,
		NodeID:     node.ID,
		NodeName:   nodeLabel(node),
		TS:         now,
		Severity:   SeverityCritical,
		PanelOK:    panelOK,
		SSHOK:      sshOK,
		PanelURL:   node.BaseURL,
		IP:         node.SSHHost,
		TargetType: "ssh",
	}
	if sshErr != nil {
		sshAlert.Error = strings.TrimSpace(*sshErr)
	}
	s.maybeSendAlert(ctx, settings, !sshOK, sshAlert)
}

func (s *Service) LoadSettingsForOrg(ctx context.Context, orgID *uuid.UUID) (*Settings, error) {
	if s == nil || s.db == nil || s.enc == nil || orgID == nil {
		return nil, nil
	}
	var row db.TelegramSettings
	err := s.db.WithContext(ctx).
		Where("org_id = ?", *orgID).
		Order("created_at desc").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return s.settingsFromRow(&row)
}

func (s *Service) NotifyCPU(ctx context.Context, settings *Settings, node *db.Node, load1 float64) {
	if settings == nil || !settings.AlertCPU || node == nil {
		return
	}
	threshold := s.policy.CPULoadThreshold
	active := load1 >= threshold
	alert := Alert{
		Type:     AlertCPU,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			Load1:     load1,
			Threshold: threshold,
		},
		PanelURL: node.BaseURL,
		IP:       node.SSHHost,
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) NotifyMemory(ctx context.Context, settings *Settings, node *db.Node, usedPercent float64) {
	if settings == nil || !settings.AlertMemory || node == nil {
		return
	}
	threshold := s.policy.MemPercentHigh
	active := usedPercent >= threshold
	alert := Alert{
		Type:     AlertMemory,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			UsedPct:   usedPercent,
			Threshold: threshold,
		},
		PanelURL: node.BaseURL,
		IP:       node.SSHHost,
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) NotifyDisk(ctx context.Context, settings *Settings, node *db.Node, freePercent float64) {
	if settings == nil || !settings.AlertDisk || node == nil {
		return
	}
	threshold := s.policy.DiskFreeLow
	active := freePercent <= threshold
	alert := Alert{
		Type:     AlertDisk,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			FreePct:   freePercent,
			Threshold: threshold,
		},
		PanelURL: node.BaseURL,
		IP:       node.SSHHost,
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) NotifyGeneric(ctx context.Context, settings *Settings, node *db.Node, service *db.Service, check *db.Check, status string, latency int, statusCode int, errMsg *string) {
	if settings == nil && s != nil && node != nil {
		settings, _ = s.LoadSettingsForOrg(ctx, node.OrgID)
	}
	if settings == nil || node == nil || check == nil {
		return
	}
	active := strings.ToLower(strings.TrimSpace(status)) != "ok"
	alert := Alert{
		Type:           AlertGeneric,
		OrgID:          node.OrgID,
		NodeID:         node.ID,
		NodeName:       nodeLabel(node),
		TS:             time.Now(),
		Severity:       SeverityCritical,
		ServiceKind:    "",
		CheckType:      strings.TrimSpace(check.Type),
		Target:         serviceTarget(node, service),
		TargetType:     "service",
		Status:         status,
		PanelURL:       node.BaseURL,
		IP:             node.SSHHost,
		FailAfterSec:   check.FailAfterSec,
		RecoverAfterOK: check.RecoverAfterOK,
		MuteUntil:      check.MuteUntil,
	}
	alert.Metrics.LatencyMS = latency
	alert.Metrics.StatusCode = statusCode
	alert.CheckID = check.ID
	if service != nil {
		alert.ServiceID = service.ID
		alert.ServiceKind = strings.TrimSpace(service.Kind)
	}
	if errMsg != nil {
		alert.Error = strings.TrimSpace(*errMsg)
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) NotifyGenericBot(ctx context.Context, settings *Settings, node *db.Node, bot *db.Bot, check *db.Check, ok bool, latency int, statusCode int, errMsg *string) {
	if settings == nil && s != nil && node != nil {
		settings, _ = s.LoadSettingsForOrg(ctx, node.OrgID)
	}
	if settings == nil || node == nil || bot == nil || check == nil {
		return
	}
	status := "ok"
	if !ok {
		status = "fail"
	}
	active := status != "ok"
	alert := Alert{
		Type:           AlertGeneric,
		OrgID:          node.OrgID,
		NodeID:         node.ID,
		BotID:          bot.ID,
		NodeName:       nodeLabel(node),
		TS:             time.Now(),
		Severity:       SeverityCritical,
		BotKind:        strings.TrimSpace(bot.Kind),
		CheckType:      strings.TrimSpace(check.Type),
		Target:         botTarget(bot),
		TargetType:     "bot",
		Status:         status,
		PanelURL:       node.BaseURL,
		IP:             node.SSHHost,
		FailAfterSec:   check.FailAfterSec,
		RecoverAfterOK: check.RecoverAfterOK,
		MuteUntil:      check.MuteUntil,
	}
	alert.Metrics.LatencyMS = latency
	alert.Metrics.StatusCode = statusCode
	alert.CheckID = check.ID
	if errMsg != nil {
		alert.Error = strings.TrimSpace(*errMsg)
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func serviceTarget(node *db.Node, service *db.Service) string {
	if service == nil {
		return ""
	}
	if service.URL != nil && strings.TrimSpace(*service.URL) != "" {
		return strings.TrimSpace(*service.URL)
	}
	host := ""
	if service.Host != nil {
		host = strings.TrimSpace(*service.Host)
	}
	if host == "" && node != nil {
		host = strings.TrimSpace(node.Host)
		if host == "" {
			host = strings.TrimSpace(node.SSHHost)
		}
	}
	if host == "" {
		return ""
	}
	port := 0
	if service.Port != nil {
		port = *service.Port
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

func botTarget(bot *db.Bot) string {
	if bot == nil {
		return ""
	}
	name := strings.TrimSpace(bot.Name)
	if bot.DockerContainer != nil && strings.TrimSpace(*bot.DockerContainer) != "" {
		container := strings.TrimSpace(*bot.DockerContainer)
		if name != "" {
			return fmt.Sprintf("%s (%s)", name, container)
		}
		return container
	}
	if bot.SystemdUnit != nil && strings.TrimSpace(*bot.SystemdUnit) != "" {
		unit := strings.TrimSpace(*bot.SystemdUnit)
		if name != "" {
			return fmt.Sprintf("%s (%s)", name, unit)
		}
		return unit
	}
	if bot.HealthURL != nil && strings.TrimSpace(*bot.HealthURL) != "" {
		raw := strings.TrimSpace(*bot.HealthURL)
		if name != "" {
			return fmt.Sprintf("%s (%s)", name, raw)
		}
		return raw
	}
	if name != "" {
		return name
	}
	return ""
}

func (s *Service) maybeSendAlert(ctx context.Context, settings *Settings, active bool, alert Alert) {
	if s == nil || s.client == nil || settings == nil {
		return
	}
	status := strings.ToLower(strings.TrimSpace(alert.Status))
	if status == "" {
		if active {
			status = "fail"
		} else {
			status = "ok"
		}
	}
	alert.Status = status
	alert.Fingerprint = fingerprintFor(alert)
	now := time.Now()
	state := s.loadState(ctx, alert.Fingerprint)
	alert.AlertID = s.ensureAlertID(ctx, state)
	if state != nil && state.IncidentID != nil {
		alert.IncidentID = state.IncidentID.String()
	}
	prevStatus := ""
	if state != nil && state.LastStatus != nil {
		prevStatus = strings.ToLower(strings.TrimSpace(*state.LastStatus))
	}
	prevMessageIDs := messageIDsFromJSONOrEmpty(state)
	prevOKStreak := 0
	if state != nil && state.OKStreak > 0 {
		prevOKStreak = state.OKStreak
	}

	failDelay := s.policy.OfflineDelay
	if alert.FailAfterSec > 0 {
		custom := time.Duration(alert.FailAfterSec) * time.Second
		if custom > failDelay {
			failDelay = custom
		}
	}
	if failDelay < 0 {
		failDelay = 0
	}
	recoverAfterOK := alert.RecoverAfterOK
	if recoverAfterOK < 1 {
		recoverAfterOK = 1
	}
	if alert.MuteUntil != nil && now.Before(*alert.MuteUntil) {
		s.updateState(ctx, alert, state, status, now, false, prevMessageIDs, prevOKStreak, incidentUUIDFromAlert(alert))
		_ = s.syncIncident(ctx, alert, now, status, prevStatus)
		return
	}

	if status == "ok" {
		if state != nil && prevStatus == "fail" {
			okStreak := prevOKStreak + 1
			if okStreak < recoverAfterOK {
				s.updateState(ctx, alert, state, "fail", now, false, prevMessageIDs, okStreak, incidentUUIDFromAlert(alert))
				_ = s.syncIncident(ctx, alert, now, "fail", prevStatus)
				return
			}
			if len(prevMessageIDs) == 0 {
				// Fail state was not surfaced to Telegram (delay window or send failed):
				// do not send a recovery notification.
				alert.Occurrences = 0
				incidentID := s.syncIncident(ctx, alert, now, "ok", prevStatus)
				s.updateState(ctx, alert, state, "ok", now, true, map[string]int{}, 0, incidentID)
				return
			}
			alert.Occurrences = state.Occurrences
			s.sendRecovery(ctx, settings, alert, state)
			// Clear thread mapping on recovery so the next incident starts a new alert message.
			incidentID := s.syncIncident(ctx, alert, now, "ok", prevStatus)
			s.updateState(ctx, alert, state, "ok", now, true, map[string]int{}, 0, incidentID)
			return
		}
		incidentID := s.syncIncident(ctx, alert, now, "ok", prevStatus)
		s.updateState(ctx, alert, state, "ok", now, false, messageIDsFromJSONOrEmpty(state), 0, incidentID)
		return
	}

	if state != nil && prevStatus == "fail" {
		alert.Occurrences = state.Occurrences + 1
		if alert.Occurrences < 1 {
			alert.Occurrences = 1
		}
	} else {
		alert.Occurrences = 1
	}

	if requiresOfflineDelay(alert.Type) && len(prevMessageIDs) == 0 {
		failStartedAt := now
		if state != nil && prevStatus == "fail" && !state.FirstSeen.IsZero() {
			failStartedAt = state.FirstSeen
		}
		if now.Sub(failStartedAt) < failDelay {
			incidentID := s.syncIncident(ctx, alert, now, status, prevStatus)
			s.updateState(ctx, alert, state, status, now, false, prevMessageIDs, 0, incidentID)
			return
		}
	}

	repeatedFailWithoutThread := false
	if state != nil && state.LastStatus != nil && *state.LastStatus == "fail" {
		existingMessageIDs := messageIDsFromJSONOrEmpty(state)
		if len(existingMessageIDs) > 0 && hasMessageThreadForAllChats(settings.AdminChatIDs, existingMessageIDs) {
			incidentID := s.syncIncident(ctx, alert, now, status, prevStatus)
			s.updateState(ctx, alert, state, status, now, false, existingMessageIDs, 0, incidentID)
			return
		}
		// If the last fail never produced a Telegram message (e.g. settings/network issue),
		// or chats were rotated and this alert has no thread in the active chats,
		// retry sending until a thread is created.
		repeatedFailWithoutThread = true
	}
	if repeatedFailWithoutThread && state != nil && alert.Occurrences < state.Occurrences {
		alert.Occurrences = state.Occurrences
	}

	if s.isMuted(ctx, alert.Fingerprint) {
		incidentID := s.syncIncident(ctx, alert, now, status, prevStatus)
		s.updateState(ctx, alert, state, status, now, false, messageIDsFromJSONOrEmpty(state), 0, incidentID)
		return
	}

	messageIDs := map[string]int{}
	if state != nil {
		messageIDs = messageIDsFromJSON(state.LastMessageIDs)
	}
	text, keyboard := RenderAlert(alert, s.publicBaseURL)
	if len(messageIDs) == 0 {
		for _, chatID := range settings.AdminChatIDs {
			msgID, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, keyboard)
			if err != nil {
				log.Printf("telegram alert failed chat_id=%s error=%v", chatID, err)
				continue
			}
			messageIDs[chatID] = msgID
		}
	} else {
		updatedMessageIDs := map[string]int{}
		for _, chatID := range settings.AdminChatIDs {
			if msgID, ok := messageIDs[chatID]; ok && msgID > 0 {
				if err := s.client.EditMessage(ctx, settings.BotToken, chatID, msgID, text, parseModeHTML, keyboard); err == nil {
					updatedMessageIDs[chatID] = msgID
					continue
				} else {
					log.Printf("telegram edit failed chat_id=%s error=%v", chatID, err)
				}
			}
			newMsgID, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, keyboard)
			if err != nil {
				log.Printf("telegram alert resend failed chat_id=%s error=%v", chatID, err)
				continue
			}
			updatedMessageIDs[chatID] = newMsgID
		}
		messageIDs = updatedMessageIDs
	}

	incidentID := s.syncIncident(ctx, alert, now, status, prevStatus)
	s.updateState(ctx, alert, state, status, now, true, messageIDs, 0, incidentID)
}

func (s *Service) SendTest(ctx context.Context, settings *Settings, msg string) error {
	if settings == nil || strings.TrimSpace(settings.BotToken) == "" || len(settings.AdminChatIDs) == 0 {
		return fmt.Errorf("telegram settings not configured")
	}
	text := escapeHTML(msg)
	for _, chatID := range settings.AdminChatIDs {
		if _, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SendTestDetailed(ctx context.Context, settings *Settings, msg string) []SendResult {
	results := make([]SendResult, 0, len(settings.AdminChatIDs))
	if settings == nil {
		return results
	}
	text := escapeHTML(msg)
	for _, chatID := range settings.AdminChatIDs {
		_, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, nil)
		if err != nil {
			results = append(results, SendResult{ChatID: chatID, OK: false, Error: err.Error()})
			continue
		}
		results = append(results, SendResult{ChatID: chatID, OK: true})
	}
	return results
}

func (s *Service) HandleCallback(ctx context.Context, token, data string) (string, error) {
	action, alertID, minutes := ParseCallbackData(data)
	switch action {
	case "mute":
		if minutes <= 0 {
			_, policyMute := s.policyForAlertID(ctx, alertID)
			minutes = policyMute
		}
		if err := s.MuteByAlertID(ctx, alertID, time.Duration(minutes)*time.Minute); err != nil {
			if strings.TrimSpace(alertID) != "" {
				s.setMute(alertID, time.Duration(minutes)*time.Minute)
				return fmt.Sprintf("Muted for %dm", minutes), nil
			}
			return "Mute failed", err
		}
		return fmt.Sprintf("Muted for %dm", minutes), nil
	case "ack":
		if err := s.AckByAlertID(ctx, alertID); err != nil {
			return "Ack failed", err
		}
		return "Acknowledged", nil
	case "open":
		if url := s.openURLForAlert(ctx, alertID); url != "" {
			return fmt.Sprintf("Open: %s", url), nil
		}
		return "Open in UI", nil
	case "retry":
		return "Retry queued", nil
	default:
		return "OK", nil
	}
}

func (s *Service) MuteFingerprint(ctx context.Context, fingerprint string, dur time.Duration) error {
	if strings.TrimSpace(fingerprint) == "" {
		return fmt.Errorf("fingerprint required")
	}
	s.setMute(fingerprint, dur)
	return nil
}

func (s *Service) AnswerCallback(ctx context.Context, token, callbackID, text string) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.AnswerCallback(ctx, token, callbackID, text)
}

func (s *Service) SendMessage(ctx context.Context, token, chatID, text string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("alerts service not configured")
	}
	t := strings.TrimSpace(token)
	c := strings.TrimSpace(chatID)
	if t == "" || c == "" {
		return fmt.Errorf("token and chat id required")
	}
	msg := strings.TrimSpace(text)
	if msg == "" {
		msg = "OK"
	}
	_, err := s.client.SendMessage(ctx, t, c, escapeHTML(msg), parseModeHTML, nil)
	return err
}

func (s *Service) setMute(fingerprint string, dur time.Duration) {
	if strings.TrimSpace(fingerprint) == "" {
		return
	}
	s.mu.Lock()
	s.mutedUntil[fingerprint] = time.Now().Add(dur)
	s.mu.Unlock()
	if s == nil || s.db == nil {
		return
	}
	until := time.Now().Add(dur)
	_ = s.db.Model(&db.AlertState{}).Where("fingerprint = ?", fingerprint).Updates(map[string]any{
		"muted_until": until,
		"updated_at":  time.Now(),
	}).Error
}

func (s *Service) MuteByAlertID(ctx context.Context, alertID string, dur time.Duration) error {
	if strings.TrimSpace(alertID) == "" {
		return fmt.Errorf("alert_id required")
	}
	state := s.loadStateByAlertID(ctx, alertID)
	if state == nil {
		return fmt.Errorf("alert not found")
	}
	s.setMute(state.Fingerprint, dur)
	return nil
}

func (s *Service) AckByAlertID(ctx context.Context, alertID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("alerts not configured")
	}
	state := s.loadStateByAlertID(ctx, alertID)
	if state == nil {
		return fmt.Errorf("alert not found")
	}
	ackMuteMinutes, _ := s.policyForState(ctx, state)
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&db.AlertState{}).Where("fingerprint = ?", state.Fingerprint).Updates(map[string]any{
		"muted_until": now.Add(time.Duration(ackMuteMinutes) * time.Minute),
		"updated_at":  now,
		"last_seen":   now,
	}).Error; err != nil {
		return err
	}
	if state.IncidentID != nil {
		by := "telegram"
		_ = s.db.WithContext(ctx).Model(&db.Incident{}).Where("id = ?", *state.IncidentID).Updates(map[string]any{
			"status":          "acked",
			"acknowledged_at": now,
			"acknowledged_by": by,
			"updated_at":      now,
		}).Error
	}
	return nil
}

func (s *Service) openURLForAlert(ctx context.Context, alertID string) string {
	if s == nil || s.db == nil || strings.TrimSpace(s.publicBaseURL) == "" {
		return ""
	}
	state := s.loadStateByAlertID(ctx, alertID)
	if state == nil || state.NodeID == nil || *state.NodeID == uuid.Nil {
		return strings.TrimRight(s.publicBaseURL, "/")
	}
	base := strings.TrimRight(s.publicBaseURL, "/")
	return fmt.Sprintf("%s/nodes?node=%s", base, state.NodeID.String())
}

func normalizePolicyMinutes(ackMuteMinutes, muteMinutes int) (int, int) {
	ack := ackMuteMinutes
	if ack <= 0 {
		ack = defaultAckMuteMinutes
	}
	if ack > maxPolicyMinutes {
		ack = maxPolicyMinutes
	}
	mute := muteMinutes
	if mute <= 0 {
		mute = defaultButtonMuteMins
	}
	if mute > maxPolicyMinutes {
		mute = maxPolicyMinutes
	}
	return ack, mute
}

func (s *Service) policyForAlertID(ctx context.Context, alertID string) (int, int) {
	state := s.loadStateByAlertID(ctx, alertID)
	return s.policyForState(ctx, state)
}

func (s *Service) policyForState(ctx context.Context, state *db.AlertState) (int, int) {
	ack, mute := normalizePolicyMinutes(0, 0)
	if s == nil || s.db == nil || state == nil {
		return ack, mute
	}
	orgID := s.orgIDForState(ctx, state)
	if orgID == nil || *orgID == uuid.Nil {
		return ack, mute
	}
	var row db.TelegramSettings
	if err := s.db.WithContext(ctx).
		Where("org_id = ?", *orgID).
		Order("created_at desc").
		First(&row).Error; err != nil {
		return ack, mute
	}
	return normalizePolicyMinutes(row.AckMuteMinutes, row.MuteMinutes)
}

func (s *Service) orgIDForState(ctx context.Context, state *db.AlertState) *uuid.UUID {
	if s == nil || s.db == nil || state == nil {
		return nil
	}
	if state.IncidentID != nil && *state.IncidentID != uuid.Nil {
		var incident db.Incident
		if err := s.db.WithContext(ctx).Select("org_id").First(&incident, "id = ?", *state.IncidentID).Error; err == nil && incident.OrgID != nil {
			id := *incident.OrgID
			return &id
		}
	}
	if state.NodeID != nil && *state.NodeID != uuid.Nil {
		var node db.Node
		if err := s.db.WithContext(ctx).Select("org_id").First(&node, "id = ?", *state.NodeID).Error; err == nil && node.OrgID != nil {
			id := *node.OrgID
			return &id
		}
	}
	if state.ServiceID != nil && *state.ServiceID != uuid.Nil {
		var svc db.Service
		if err := s.db.WithContext(ctx).Select("node_id").First(&svc, "id = ?", *state.ServiceID).Error; err == nil {
			var node db.Node
			if err := s.db.WithContext(ctx).Select("org_id").First(&node, "id = ?", svc.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	if state.BotID != nil && *state.BotID != uuid.Nil {
		var bot db.Bot
		if err := s.db.WithContext(ctx).Select("node_id").First(&bot, "id = ?", *state.BotID).Error; err == nil {
			var node db.Node
			if err := s.db.WithContext(ctx).Select("org_id").First(&node, "id = ?", bot.NodeID).Error; err == nil && node.OrgID != nil {
				id := *node.OrgID
				return &id
			}
		}
	}
	return nil
}

func panelTargetType(code *string) string {
	if code == nil {
		return "panel"
	}
	switch strings.TrimSpace(*code) {
	case "CERT_EXPIRED", "CERT_NOT_YET_VALID", "UNKNOWN_CA", "HOSTNAME_MISMATCH", "HANDSHAKE":
		return "tls"
	case "GENERIC_HTTP_ERROR":
		return "http"
	default:
		return "panel"
	}
}

func tlsCodeFromPanelCode(code *string) string {
	if code == nil {
		return ""
	}
	switch strings.TrimSpace(*code) {
	case "CERT_EXPIRED", "CERT_NOT_YET_VALID", "UNKNOWN_CA", "HOSTNAME_MISMATCH", "HANDSHAKE":
		return strings.TrimSpace(*code)
	default:
		return ""
	}
}
func (s *Service) isMuted(ctx context.Context, fingerprint string) bool {
	if strings.TrimSpace(fingerprint) == "" {
		return false
	}
	s.mu.Lock()
	until, ok := s.mutedUntil[fingerprint]
	if ok && time.Now().Before(until) {
		s.mu.Unlock()
		return true
	}
	if ok {
		delete(s.mutedUntil, fingerprint)
	}
	s.mu.Unlock()
	if s == nil || s.db == nil {
		return false
	}
	var state db.AlertState
	err := s.db.WithContext(ctx).Where("fingerprint = ? AND muted_until > ?", fingerprint, time.Now()).First(&state).Error
	if err != nil {
		return false
	}
	if state.MutedUntil == nil {
		return false
	}
	s.mu.Lock()
	s.mutedUntil[fingerprint] = *state.MutedUntil
	s.mu.Unlock()
	return true
}
func splitChatIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	var out []string
	for _, part := range parts {
		val := strings.TrimSpace(part)
		if val != "" {
			out = append(out, val)
		}
	}
	return out
}

func nodeLabel(node *db.Node) string {
	if node == nil {
		return "unknown"
	}
	if strings.TrimSpace(node.Name) != "" {
		return node.Name
	}
	if node.ID != uuid.Nil {
		return node.ID.String()
	}
	return "unknown"
}

func CPUThreshold() float64 {
	return defaultPolicy().CPULoadThreshold
}

func MemThreshold() float64 {
	return defaultPolicy().MemPercentHigh
}

func DiskThreshold() float64 {
	return defaultPolicy().DiskFreeLow
}

func (s *Service) loadState(ctx context.Context, fingerprint string) *db.AlertState {
	if s == nil || s.db == nil {
		return nil
	}
	var state db.AlertState
	err := s.db.WithContext(ctx).First(&state, "fingerprint = ?", fingerprint).Error
	if err != nil {
		return nil
	}
	return &state
}

func (s *Service) loadStateByAlertID(ctx context.Context, alertID string) *db.AlertState {
	if s == nil || s.db == nil || strings.TrimSpace(alertID) == "" {
		return nil
	}
	id, err := uuid.Parse(alertID)
	if err != nil {
		return nil
	}
	var state db.AlertState
	err = s.db.WithContext(ctx).Where("alert_id = ?", id).First(&state).Error
	if err != nil {
		return nil
	}
	return &state
}

func (s *Service) ensureAlertID(ctx context.Context, state *db.AlertState) string {
	if state != nil && state.AlertID != nil {
		return state.AlertID.String()
	}
	id := uuid.New()
	if state != nil && s != nil && s.db != nil {
		_ = s.db.WithContext(ctx).Model(&db.AlertState{}).
			Where("fingerprint = ?", state.Fingerprint).
			Update("alert_id", id).Error
		state.AlertID = &id
	}
	return id.String()
}

func (s *Service) updateState(ctx context.Context, alert Alert, state *db.AlertState, status string, now time.Time, notified bool, messageIDs map[string]int, okStreak int, incidentID *uuid.UUID) {
	if s == nil || s.db == nil {
		return
	}
	payload := map[string]any{
		"alert_type":  string(alert.Type),
		"node_id":     nullableUUID(alert.NodeID),
		"service_id":  nullableUUID(alert.ServiceID),
		"bot_id":      nullableUUID(alert.BotID),
		"check_type":  nullableString(alert.CheckType),
		"last_status": status,
		"last_seen":   now,
		"occurrences": alert.Occurrences,
		"ok_streak":   okStreak,
	}
	if incidentID != nil && *incidentID != uuid.Nil {
		payload["incident_id"] = *incidentID
	}
	if notified {
		payload["updated_at"] = now
		payload["last_message_ids"] = messageIDsToJSON(messageIDs)
	}
	if state != nil && state.AlertID == nil && strings.TrimSpace(alert.AlertID) != "" {
		if id, err := uuid.Parse(alert.AlertID); err == nil {
			payload["alert_id"] = id
		}
	}
	if state != nil && status == "fail" {
		prev := ""
		if state.LastStatus != nil {
			prev = strings.ToLower(strings.TrimSpace(*state.LastStatus))
		}
		if prev != "fail" {
			payload["first_seen"] = now
		}
	}
	if state != nil {
		_ = s.db.WithContext(ctx).Model(&db.AlertState{}).Where("fingerprint = ?", alert.Fingerprint).Updates(payload).Error
		return
	}
	var alertID uuid.UUID
	if strings.TrimSpace(alert.AlertID) != "" {
		if id, err := uuid.Parse(alert.AlertID); err == nil {
			alertID = id
		}
	}
	if alertID == uuid.Nil {
		alertID = uuid.New()
	}
	row := db.AlertState{
		AlertID:        &alertID,
		Fingerprint:    alert.Fingerprint,
		IncidentID:     incidentID,
		AlertType:      string(alert.Type),
		NodeID:         nullableUUID(alert.NodeID),
		ServiceID:      nullableUUID(alert.ServiceID),
		BotID:          nullableUUID(alert.BotID),
		CheckType:      nullableString(alert.CheckType),
		LastStatus:     &status,
		FirstSeen:      now,
		LastSeen:       now,
		Occurrences:    alert.Occurrences,
		OKStreak:       okStreak,
		LastMessageIDs: messageIDsToJSON(messageIDs),
		UpdatedAt:      now,
	}
	_ = s.db.WithContext(ctx).Create(&row).Error
}

func incidentUUIDFromAlert(alert Alert) *uuid.UUID {
	if strings.TrimSpace(alert.IncidentID) == "" {
		return nil
	}
	id, err := uuid.Parse(strings.TrimSpace(alert.IncidentID))
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func (s *Service) syncIncident(ctx context.Context, alert Alert, now time.Time, status, prevStatus string) *uuid.UUID {
	if s == nil || s.db == nil {
		return nil
	}
	fp := strings.TrimSpace(alert.Fingerprint)
	if fp == "" {
		return nil
	}

	var incident db.Incident
	err := s.db.WithContext(ctx).Where("fingerprint = ?", fp).First(&incident).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		if status != "fail" {
			return nil
		}
		desc := strings.TrimSpace(alert.Error)
		rec := db.Incident{
			OrgID:       alert.OrgID,
			Fingerprint: fp,
			AlertType:   string(alert.Type),
			Severity:    string(alert.Severity),
			Status:      "open",
			NodeID:      nullableUUID(alert.NodeID),
			ServiceID:   nullableUUID(alert.ServiceID),
			BotID:       nullableUUID(alert.BotID),
			CheckID:     nullableUUID(alert.CheckID),
			Title:       strings.TrimSpace(renderTitle(alert)),
			FirstSeen:   now,
			LastSeen:    now,
			Occurrences: maxInt(alert.Occurrences, 1),
			UpdatedAt:   now,
		}
		if desc != "" {
			rec.Description = &desc
			rec.LastError = &desc
		}
		if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
			return nil
		}
		return &rec.ID
	case err != nil:
		return nil
	}

	incidentID := incident.ID
	updates := map[string]any{
		"last_seen":   now,
		"updated_at":  now,
		"org_id":      alert.OrgID,
		"alert_type":  string(alert.Type),
		"severity":    string(alert.Severity),
		"node_id":     nullableUUID(alert.NodeID),
		"service_id":  nullableUUID(alert.ServiceID),
		"bot_id":      nullableUUID(alert.BotID),
		"check_id":    nullableUUID(alert.CheckID),
		"title":       strings.TrimSpace(renderTitle(alert)),
		"description": strings.TrimSpace(alert.Error),
		"occurrences": maxInt(alert.Occurrences, incident.Occurrences),
		"last_error":  strings.TrimSpace(alert.Error),
		"fingerprint": fp,
	}
	if status == "fail" {
		updates["status"] = "open"
		updates["recovered_at"] = nil
		if prevStatus != "fail" || incident.Status == "recovered" {
			updates["first_seen"] = now
			updates["acknowledged_at"] = nil
			updates["acknowledged_by"] = nil
		}
	} else {
		if incident.Status != "recovered" {
			updates["status"] = "recovered"
			updates["recovered_at"] = now
		}
	}
	if err := s.db.WithContext(ctx).Model(&db.Incident{}).Where("id = ?", incidentID).Updates(updates).Error; err != nil {
		return nil
	}
	return &incidentID
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func (s *Service) sendRecovery(ctx context.Context, settings *Settings, alert Alert, state *db.AlertState) {
	if settings == nil {
		return
	}
	text, keyboard := RenderRecovery(alert, s.publicBaseURL)
	for _, chatID := range settings.AdminChatIDs {
		if _, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, keyboard); err != nil {
			log.Printf("telegram recovery resend failed chat_id=%s error=%v", chatID, err)
		}
	}
}

func messageIDsFromJSON(raw datatypes.JSON) map[string]int {
	if len(raw) == 0 {
		return map[string]int{}
	}
	var list []alertMessageID
	if err := json.Unmarshal(raw, &list); err != nil {
		var legacy map[string]int
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return map[string]int{}
		}
		return legacy
	}
	out := map[string]int{}
	for _, item := range list {
		if strings.TrimSpace(item.ChatID) == "" || item.MessageID == 0 {
			continue
		}
		out[item.ChatID] = item.MessageID
	}
	return out
}

func messageIDsToJSON(values map[string]int) datatypes.JSON {
	if values == nil {
		return datatypes.JSON([]byte("[]"))
	}
	list := make([]alertMessageID, 0, len(values))
	for chatID, msgID := range values {
		if strings.TrimSpace(chatID) == "" || msgID == 0 {
			continue
		}
		list = append(list, alertMessageID{ChatID: chatID, MessageID: msgID})
	}
	b, err := json.Marshal(list)
	if err != nil {
		return datatypes.JSON([]byte("[]"))
	}
	return datatypes.JSON(b)
}

func messageIDsFromJSONOrEmpty(state *db.AlertState) map[string]int {
	if state == nil {
		return map[string]int{}
	}
	return messageIDsFromJSON(state.LastMessageIDs)
}

func requiresOfflineDelay(alertType AlertType) bool {
	return alertType == AlertGeneric || alertType == AlertConnection
}

func hasMessageThreadForAllChats(chatIDs []string, messageIDs map[string]int) bool {
	if len(chatIDs) == 0 || len(messageIDs) == 0 {
		return false
	}
	for _, chatID := range chatIDs {
		if strings.TrimSpace(chatID) == "" {
			continue
		}
		if msgID, ok := messageIDs[chatID]; !ok || msgID <= 0 {
			return false
		}
	}
	return true
}

func nullableString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	v := strings.TrimSpace(value)
	return &v
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	v := id
	return &v
}
