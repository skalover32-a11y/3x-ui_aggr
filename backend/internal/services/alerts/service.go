package alerts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

const (
	cooldownDuration = 30 * time.Minute
	cpuLoadThreshold = 2.0
	memPercentHigh   = 90.0
	diskFreeLow      = 10.0
)

type Service struct {
	db     *gorm.DB
	enc    *security.Encryptor
	client *http.Client
	mu     sync.Mutex
	state  map[string]*alertState
}

type alertState struct {
	active   bool
	lastSent time.Time
}

type Settings struct {
	BotToken        string
	AdminChatID     string
	AlertConnection bool
	AlertCPU        bool
	AlertMemory     bool
	AlertDisk       bool
}

func New(dbConn *gorm.DB, enc *security.Encryptor) *Service {
	return &Service{
		db:     dbConn,
		enc:    enc,
		client: &http.Client{Timeout: 5 * time.Second},
		state:  map[string]*alertState{},
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
	return &Settings{
		BotToken:        token,
		AdminChatID:     strings.TrimSpace(row.AdminChatID),
		AlertConnection: row.AlertConnection,
		AlertCPU:        row.AlertCPU,
		AlertMemory:     row.AlertMemory,
		AlertDisk:       row.AlertDisk,
	}, nil
}

func (s *Service) NotifyConnection(ctx context.Context, settings *Settings, nodeName string, panelOK, sshOK bool, errMsg *string) {
	if settings == nil || !settings.AlertConnection {
		return
	}
	active := !(panelOK && sshOK)
	msg := fmt.Sprintf("🚨 Connection issue on %s\npanel_ok=%t\nssh_ok=%t", nodeName, panelOK, sshOK)
	if errMsg != nil && strings.TrimSpace(*errMsg) != "" {
		msg += fmt.Sprintf("\nerror: %s", *errMsg)
	}
	s.maybeSend(ctx, "conn:"+nodeName, settings, active, msg)
}

func (s *Service) NotifyCPU(ctx context.Context, settings *Settings, nodeName string, load1 float64) {
	if settings == nil || !settings.AlertCPU {
		return
	}
	active := load1 >= cpuLoadThreshold
	msg := fmt.Sprintf("🚨 High CPU on %s\nload1=%.2f (threshold %.2f)", nodeName, load1, cpuLoadThreshold)
	s.maybeSend(ctx, "cpu:"+nodeName, settings, active, msg)
}

func (s *Service) NotifyMemory(ctx context.Context, settings *Settings, nodeName string, usedPercent float64) {
	if settings == nil || !settings.AlertMemory {
		return
	}
	active := usedPercent >= memPercentHigh
	msg := fmt.Sprintf("🚨 High memory on %s\nused=%.1f%% (threshold %.1f%%)", nodeName, usedPercent, memPercentHigh)
	s.maybeSend(ctx, "mem:"+nodeName, settings, active, msg)
}

func (s *Service) NotifyDisk(ctx context.Context, settings *Settings, nodeName string, freePercent float64) {
	if settings == nil || !settings.AlertDisk {
		return
	}
	active := freePercent <= diskFreeLow
	msg := fmt.Sprintf("🚨 Low disk space on %s\nfree=%.1f%% (threshold %.1f%%)", nodeName, freePercent, diskFreeLow)
	s.maybeSend(ctx, "disk:"+nodeName, settings, active, msg)
}

func (s *Service) maybeSend(ctx context.Context, key string, settings *Settings, active bool, msg string) {
	s.mu.Lock()
	state := s.state[key]
	if state == nil {
		state = &alertState{}
		s.state[key] = state
	}
	shouldSend := false
	if active {
		if !state.active {
			shouldSend = true
		} else if time.Since(state.lastSent) >= cooldownDuration {
			shouldSend = true
		}
	} else {
		state.active = false
	}
	s.mu.Unlock()

	if !active || !shouldSend {
		return
	}
	if err := s.sendMessage(ctx, settings.BotToken, settings.AdminChatID, msg); err != nil {
		return
	}
	s.mu.Lock()
	state.active = true
	state.lastSent = time.Now()
	s.mu.Unlock()
}

func (s *Service) sendMessage(ctx context.Context, token, chatID, text string) error {
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	values := url.Values{}
	values.Set("chat_id", chatID)
	values.Set("text", text)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("telegram send failed: %s", resp.Status)
	}
	return nil
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
