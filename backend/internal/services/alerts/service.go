package alerts

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

const (
	dedupTTL         = 10 * time.Minute
	cpuLoadThreshold = 2.0
	memPercentHigh   = 90.0
	diskFreeLow      = 10.0
)

type Service struct {
	db            *gorm.DB
	enc           *security.Encryptor
	publicBaseURL string
	client        *telegramClient
	dedup         *Deduper
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
}

type SendResult struct {
	ChatID string `json:"chat_id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

func New(dbConn *gorm.DB, enc *security.Encryptor, publicBaseURL string) *Service {
	return &Service{
		db:            dbConn,
		enc:           enc,
		publicBaseURL: strings.TrimSpace(publicBaseURL),
		client:        newTelegramClient(),
		dedup:         NewDeduper(dedupTTL),
		mutedUntil:    map[string]time.Time{},
	}
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
	return &Settings{
		BotToken:        token,
		AdminChatIDs:    adminIDs,
		AlertConnection: row.AlertConnection,
		AlertCPU:        row.AlertCPU,
		AlertMemory:     row.AlertMemory,
		AlertDisk:       row.AlertDisk,
	}, nil
}

func (s *Service) NotifyConnection(ctx context.Context, settings *Settings, node *db.Node, panelOK, sshOK bool, errMsg *string) {
	if settings == nil || !settings.AlertConnection || node == nil {
		return
	}
	active := !(panelOK && sshOK)
	alert := Alert{
		Type:     AlertConnection,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityCritical,
		PanelOK:  panelOK,
		SSHOK:    sshOK,
		PanelURL: node.BaseURL,
		IP:       node.SSHHost,
	}
	if errMsg != nil {
		alert.Error = strings.TrimSpace(*errMsg)
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) NotifyCPU(ctx context.Context, settings *Settings, node *db.Node, load1 float64) {
	if settings == nil || !settings.AlertCPU || node == nil {
		return
	}
	active := load1 >= cpuLoadThreshold
	alert := Alert{
		Type:     AlertCPU,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			Load1:     load1,
			Threshold: cpuLoadThreshold,
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
	active := usedPercent >= memPercentHigh
	alert := Alert{
		Type:     AlertMemory,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			UsedPct:   usedPercent,
			Threshold: memPercentHigh,
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
	active := freePercent <= diskFreeLow
	alert := Alert{
		Type:     AlertDisk,
		NodeID:   node.ID,
		NodeName: nodeLabel(node),
		TS:       time.Now(),
		Severity: SeverityWarning,
		Metrics: AlertMetrics{
			FreePct:   freePercent,
			Threshold: diskFreeLow,
		},
		PanelURL: node.BaseURL,
		IP:       node.SSHHost,
	}
	s.maybeSendAlert(ctx, settings, active, alert)
}

func (s *Service) maybeSendAlert(ctx context.Context, settings *Settings, active bool, alert Alert) {
	if s == nil || s.client == nil || s.dedup == nil {
		return
	}
	if !active {
		s.dedup.ClearByPrefix(fmt.Sprintf("%s|%s", alert.Type, strings.ToLower(alert.NodeName)))
		return
	}
	if s.isMuted(alert.NodeID.String()) {
		return
	}
	alert.AlertID = uuid.NewString()
	alert.Fingerprint = fingerprintFor(alert)
	send, entry := s.dedup.Track(alert, time.Now())
	if entry != nil {
		alert.Occurrences = entry.occurrences
	}
	text, keyboard := RenderAlert(alert, s.publicBaseURL)
	if send {
		messageIDs := map[string]int{}
		for _, chatID := range settings.AdminChatIDs {
			msgID, err := s.client.SendMessage(ctx, settings.BotToken, chatID, text, parseModeHTML, keyboard)
			if err != nil {
				log.Printf("telegram alert failed chat_id=%s error=%v", chatID, err)
				continue
			}
			messageIDs[chatID] = msgID
		}
		s.dedup.RecordSend(alert.Fingerprint, messageIDs)
		return
	}
	messageIDs := s.dedup.MessageIDs(alert.Fingerprint)
	if len(messageIDs) == 0 {
		return
	}
	for chatID, msgID := range messageIDs {
		if err := s.client.EditMessage(ctx, settings.BotToken, chatID, msgID, text, parseModeHTML, keyboard); err != nil {
			log.Printf("telegram edit failed chat_id=%s error=%v", chatID, err)
		}
	}
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
	if strings.HasPrefix(data, "mute:") {
		parts := strings.Split(data, ":")
		if len(parts) >= 3 && parts[1] == "1h" {
			s.setMute(parts[2], time.Hour)
			return "Muted for 1h", nil
		}
		return "Mute updated", nil
	}
	if strings.HasPrefix(data, "retry:") {
		return "Retry requested", nil
	}
	return "OK", nil
}

func (s *Service) AnswerCallback(ctx context.Context, token, callbackID, text string) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.AnswerCallback(ctx, token, callbackID, text)
}

func (s *Service) setMute(nodeID string, dur time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutedUntil[nodeID] = time.Now().Add(dur)
}

func (s *Service) isMuted(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.mutedUntil[nodeID]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(s.mutedUntil, nodeID)
		return false
	}
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
	return cpuLoadThreshold
}

func MemThreshold() float64 {
	return memPercentHigh
}

func DiskThreshold() float64 {
	return diskFreeLow
}
