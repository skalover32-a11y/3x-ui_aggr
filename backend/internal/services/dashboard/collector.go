package dashboard

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"agr_3x_ui/internal/db"
)

type Service struct {
	DB          *gorm.DB
	Metrics     NodeMetricsProvider
	Users       ActiveUsersProvider
	Hub         *Hub
	Interval    time.Duration
	Parallelism int
	stop        chan struct{}
	stopOnce    sync.Once
}

func New(dbConn *gorm.DB, metrics NodeMetricsProvider, users ActiveUsersProvider, interval time.Duration, parallelism int) *Service {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if parallelism <= 0 {
		parallelism = 5
	}
	return &Service{
		DB:          dbConn,
		Metrics:     metrics,
		Users:       users,
		Hub:         NewHub(),
		Interval:    interval,
		Parallelism: parallelism,
		stop:        make(chan struct{}),
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.DB == nil || s.Metrics == nil {
		return
	}
	go s.loop(ctx)
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

func (s *Service) Subscribe() (<-chan Event, func()) {
	if s == nil || s.Hub == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	return s.Hub.Subscribe()
}

func (s *Service) loop(ctx context.Context) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.collectOnce(ctx)
		}
	}
}

func (s *Service) collectOnce(ctx context.Context) {
	var nodes []db.Node
	if err := s.DB.WithContext(ctx).Where("is_enabled = true").Find(&nodes).Error; err != nil {
		return
	}
	if len(nodes) == 0 {
		return
	}
	parallelism := s.Parallelism
	if parallelism <= 0 {
		parallelism = 5
	}
	ch := make(chan db.Node)
	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range ch {
				s.collectForNode(ctx, &node)
			}
		}()
	}
	for _, node := range nodes {
		ch <- node
	}
	close(ch)
	wg.Wait()
}

func (s *Service) collectForNode(ctx context.Context, node *db.Node) {
	if node == nil || !node.SSHEnabled {
		return
	}
	metrics, err := s.Metrics.CollectNodeMetrics(ctx, node)
	if err == nil {
		_ = s.upsertMetrics(ctx, node.ID, metrics)
		s.Hub.Publish(newEvent(EventNodeMetricsUpdate, map[string]any{
			"node_id": node.ID.String(),
			"metrics": toMetricsPayload(metrics),
		}))
	}
	if s.Users != nil {
		usersResult, err := s.Users.CollectActiveUsers(ctx, node)
		if err == nil {
			_ = s.replaceActiveUsers(ctx, node.ID, usersResult.Users)
			s.Hub.Publish(newEvent(EventActiveUsersUpdate, map[string]any{
				"node_id":   node.ID.String(),
				"node_name": node.Name,
				"users":     usersResult.Users,
				"source":    usersResult.Source,
			}))
		}
	}
}

func (s *Service) upsertMetrics(ctx context.Context, nodeID uuid.UUID, metrics NodeMetrics) error {
	row := db.NodeMetricsLatest{
		NodeID:         nodeID,
		CollectedAt:    metrics.CollectedAt,
		CPUPct:         metrics.CPUPct,
		RAMUsedBytes:   metrics.RAMUsedBytes,
		RAMTotalBytes:  metrics.RAMTotalBytes,
		DiskUsedBytes:  metrics.DiskUsedBytes,
		DiskTotalBytes: metrics.DiskTotalBytes,
		NetRxBps:       metrics.NetRxBps,
		NetTxBps:       metrics.NetTxBps,
		NetRxBytes:     metrics.NetRxBytes,
		NetTxBytes:     metrics.NetTxBytes,
		UptimeSec:      metrics.UptimeSec,
		PanelVersion:   metrics.PanelVersion,
		XrayRunning:    metrics.XrayRunning,
		PanelRunning:   metrics.PanelRunning,
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}},
		UpdateAll: true,
	}).Create(&row).Error
}

func (s *Service) replaceActiveUsers(ctx context.Context, nodeID uuid.UUID, users []ActiveUser) error {
	now := time.Now()
	rows := make([]db.ActiveUserLatest, 0, len(users))
	for _, user := range users {
		email := strings.TrimSpace(user.ClientEmail)
		if email == "" {
			continue
		}
		ip := strings.TrimSpace(user.IP)
		rows = append(rows, db.ActiveUserLatest{
			NodeID:         nodeID,
			InboundTag:     user.InboundTag,
			ClientEmail:    email,
			IP:             ip,
			RxBps:          user.RxBps,
			TxBps:          user.TxBps,
			TotalUpBytes:   user.TotalUpBytes,
			TotalDownBytes: user.TotalDownBytes,
			LastSeen:       user.LastSeen,
			CollectedAt:    now,
		})
	}
	if len(rows) > 0 {
		if err := s.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_id"}, {Name: "inbound_tag"}, {Name: "client_email"}, {Name: "ip"}},
			UpdateAll: true,
		}).Create(&rows).Error; err != nil {
			return err
		}
	}
	staleAfter := 2 * s.Interval
	if staleAfter < 60*time.Second {
		staleAfter = 60 * time.Second
	}
	cutoff := now.Add(-staleAfter)
	return s.DB.WithContext(ctx).Where("node_id = ? AND collected_at < ?", nodeID, cutoff).Delete(&db.ActiveUserLatest{}).Error
}

func (s *Service) LoadSnapshot(ctx context.Context) (map[string]any, error) {
	nodes, err := s.loadNodesWithMetrics(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.listActiveUsers(ctx, 200, "")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"nodes":        nodes,
		"active_users": users,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) loadNodesWithMetrics(ctx context.Context) ([]DashboardNode, error) {
	var rows []DashboardNode
	err := s.DB.WithContext(ctx).
		Table("nodes n").
		Select(`n.id as node_id, n.name, n.kind, n.is_enabled, n.is_sandbox,
			m.collected_at, m.cpu_pct, m.ram_used_bytes, m.ram_total_bytes,
			m.disk_used_bytes, m.disk_total_bytes, m.net_rx_bps, m.net_tx_bps,
			m.net_rx_bytes, m.net_tx_bytes, m.uptime_sec, m.panel_version,
			m.xray_running, m.panel_running`).
		Joins("LEFT JOIN node_metrics_latest m ON m.node_id = n.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) listActiveUsers(ctx context.Context, limit int, search string) ([]DashboardActiveUser, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	query := s.DB.WithContext(ctx).Table("active_users_latest a").
		Select(`a.id, a.node_id, n.name as node_name, a.inbound_tag, a.client_email, a.ip,
			a.rx_bps, a.tx_bps, a.total_up_bytes, a.total_down_bytes, a.last_seen, a.collected_at`).
		Joins("JOIN nodes n ON n.id = a.node_id").
		Order("a.last_seen DESC").
		Limit(limit)
	if strings.TrimSpace(search) != "" {
		query = query.Where("a.client_email ILIKE ?", "%"+search+"%")
	}
	var rows []DashboardActiveUser
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

type DashboardNode struct {
	NodeID         uuid.UUID  `json:"node_id"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	IsEnabled      bool       `json:"is_enabled"`
	IsSandbox      bool       `json:"is_sandbox"`
	CollectedAt    *time.Time `json:"collected_at"`
	CPUPct         *float64   `json:"cpu_pct"`
	RAMUsedBytes   *int64     `json:"ram_used_bytes"`
	RAMTotalBytes  *int64     `json:"ram_total_bytes"`
	DiskUsedBytes  *int64     `json:"disk_used_bytes"`
	DiskTotalBytes *int64     `json:"disk_total_bytes"`
	NetRxBps       *int64     `json:"net_rx_bps"`
	NetTxBps       *int64     `json:"net_tx_bps"`
	NetRxBytes     *int64     `json:"net_rx_bytes"`
	NetTxBytes     *int64     `json:"net_tx_bytes"`
	UptimeSec      *int64     `json:"uptime_sec"`
	PanelVersion   *string    `json:"panel_version"`
	XrayRunning    *bool      `json:"xray_running"`
	PanelRunning   *bool      `json:"panel_running"`
}

type DashboardActiveUser struct {
	ID             uuid.UUID `json:"id"`
	NodeID         uuid.UUID `json:"node_id"`
	NodeName       string    `json:"node_name"`
	InboundTag     *string   `json:"inbound_tag"`
	ClientEmail    string    `json:"client_email"`
	IP             string    `json:"ip"`
	RxBps          *int64    `json:"rx_bps"`
	TxBps          *int64    `json:"tx_bps"`
	TotalUpBytes   *int64    `json:"total_up_bytes"`
	TotalDownBytes *int64    `json:"total_down_bytes"`
	LastSeen       time.Time `json:"last_seen"`
	CollectedAt    time.Time `json:"collected_at"`
}

func (s *Service) Summary(ctx context.Context) (map[string]any, error) {
	nodes, err := s.loadNodesWithMetrics(ctx)
	if err != nil {
		return nil, err
	}
	agg := computeAggregate(nodes)
	return map[string]any{
		"nodes":     nodes,
		"aggregate": agg,
	}, nil
}

func (s *Service) ActiveUsers(ctx context.Context, limit int, search string) ([]DashboardActiveUser, error) {
	return s.listActiveUsers(ctx, limit, search)
}

type AggregateSummary struct {
	NodesTotal  int     `json:"nodes_total"`
	NodesOnline int     `json:"nodes_online"`
	AvgCPU      float64 `json:"avg_cpu"`
	TotalRxBps  int64   `json:"total_rx_bps"`
	TotalTxBps  int64   `json:"total_tx_bps"`
	ActiveUsers int     `json:"active_users"`
}

func computeAggregate(nodes []DashboardNode) AggregateSummary {
	var agg AggregateSummary
	var cpuCount int
	var cpuSum float64
	for _, node := range nodes {
		agg.NodesTotal++
		if node.CollectedAt != nil {
			agg.NodesOnline++
		}
		if node.CPUPct != nil {
			cpuCount++
			cpuSum += *node.CPUPct
		}
		if node.NetRxBps != nil {
			agg.TotalRxBps += *node.NetRxBps
		}
		if node.NetTxBps != nil {
			agg.TotalTxBps += *node.NetTxBps
		}
	}
	if cpuCount > 0 {
		agg.AvgCPU = cpuSum / float64(cpuCount)
	}
	return agg
}

func toMetricsPayload(metrics NodeMetrics) map[string]any {
	return map[string]any{
		"collected_at":     metrics.CollectedAt,
		"cpu_pct":          metrics.CPUPct,
		"ram_used_bytes":   metrics.RAMUsedBytes,
		"ram_total_bytes":  metrics.RAMTotalBytes,
		"disk_used_bytes":  metrics.DiskUsedBytes,
		"disk_total_bytes": metrics.DiskTotalBytes,
		"net_rx_bps":       metrics.NetRxBps,
		"net_tx_bps":       metrics.NetTxBps,
		"net_rx_bytes":     metrics.NetRxBytes,
		"net_tx_bytes":     metrics.NetTxBytes,
		"uptime_sec":       metrics.UptimeSec,
		"panel_version":    metrics.PanelVersion,
		"xray_running":     metrics.XrayRunning,
		"panel_running":    metrics.PanelRunning,
	}
}
