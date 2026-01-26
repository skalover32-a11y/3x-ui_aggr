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

func computeAgentOnline(lastSeen *time.Time, installed bool, ttl time.Duration) bool {
	if !installed || lastSeen == nil {
		return false
	}
	return time.Since(*lastSeen) <= ttl
}

type Service struct {
	DB          *gorm.DB
	Metrics     NodeMetricsProvider
	Users       ActiveUsersProvider
	Hub         *Hub
	Interval    time.Duration
	Parallelism int
	stop        chan struct{}
	stopOnce    sync.Once
	sourceMu    sync.RWMutex
	sources     map[uuid.UUID]ActiveUsersSource
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
		sources:     make(map[uuid.UUID]ActiveUsersSource),
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
	if node == nil {
		return
	}
	metrics, err := s.Metrics.CollectNodeMetrics(ctx, node)
	if err == nil {
		_ = s.upsertMetrics(ctx, node.ID, metrics)
		_ = s.insertMetricSample(ctx, node.ID, metrics)
		if metrics.FromAgent && node.AgentEnabled {
			_ = s.updateAgentLastSeen(ctx, node.ID, metrics.CollectedAt)
			_ = s.updateAgentInstalled(ctx, node.ID, true)
			if metrics.AgentVersion != nil {
				_ = s.updateAgentVersion(ctx, node.ID, *metrics.AgentVersion)
			}
			if metrics.PanelVersion != nil {
				_ = s.updateNodePanelVersion(ctx, node.ID, *metrics.PanelVersion)
			}
		}
		payload := toMetricsPayload(metrics)
		if metrics.FromAgent {
			payload["agent_last_seen_at"] = metrics.CollectedAt
			payload["agent_online"] = true
			payload["agent_installed"] = true
			payload["agent_version"] = metrics.AgentVersion
		}
		s.Hub.Publish(newEvent(EventNodeMetricsUpdate, map[string]any{
			"node_id": node.ID.String(),
			"metrics": payload,
		}))
	}
	if s.Users != nil {
		usersResult, err := s.Users.CollectActiveUsers(ctx, node)
		if err != nil {
			usersResult = ActiveUsersResult{
				Users:        nil,
				Source:       "unknown",
				SourceDetail: err.Error(),
				Available:    false,
			}
		}
		_ = s.replaceActiveUsers(ctx, node.ID, usersResult.Users)
		if usersResult.Source == "agent" && usersResult.Available && node.AgentEnabled {
			_ = s.updateAgentLastSeen(ctx, node.ID, time.Now())
		}
		s.setActiveUsersSource(node.ID, usersResult.Source, usersResult.SourceDetail, usersResult.Available)
		s.Hub.Publish(newEvent(EventActiveUsersUpdate, map[string]any{
			"node_id":       node.ID.String(),
			"node_name":     node.Name,
			"users":         usersResult.Users,
			"source":        usersResult.Source,
			"source_detail": usersResult.SourceDetail,
			"available":     usersResult.Available,
		}))
	}
}

func (s *Service) updateAgentLastSeen(ctx context.Context, nodeID uuid.UUID, ts time.Time) error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("agent_last_seen_at", ts).Error
}

func (s *Service) updateAgentInstalled(ctx context.Context, nodeID uuid.UUID, installed bool) error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("agent_installed", installed).Error
}

func (s *Service) updateAgentVersion(ctx context.Context, nodeID uuid.UUID, version string) error {
	if s == nil || s.DB == nil || strings.TrimSpace(version) == "" {
		return nil
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("agent_version", version).Error
}

func (s *Service) updateNodePanelVersion(ctx context.Context, nodeID uuid.UUID, version string) error {
	if s == nil || s.DB == nil || strings.TrimSpace(version) == "" {
		return nil
	}
	updates := map[string]any{
		"panel_version":       version,
		"versions_checked_at": time.Now().UTC(),
	}
	return s.DB.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Updates(updates).Error
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
		NetIface:       metrics.NetIface,
		UptimeSec:      metrics.UptimeSec,
		XrayRunning:    metrics.XrayRunning,
		PanelRunning:   metrics.PanelRunning,
		PingMs:         metrics.PingMs,
		TCPConnections: metrics.TCPConnections,
		UDPConnections: metrics.UDPConnections,
	}
	assignments := map[string]any{
		"collected_at":     row.CollectedAt,
		"cpu_pct":          row.CPUPct,
		"ram_used_bytes":   row.RAMUsedBytes,
		"ram_total_bytes":  row.RAMTotalBytes,
		"disk_used_bytes":  row.DiskUsedBytes,
		"disk_total_bytes": row.DiskTotalBytes,
		"net_rx_bps":       row.NetRxBps,
		"net_tx_bps":       row.NetTxBps,
		"net_rx_bytes":     row.NetRxBytes,
		"net_tx_bytes":     row.NetTxBytes,
		"net_iface":        row.NetIface,
		"uptime_sec":       row.UptimeSec,
		"xray_running":     row.XrayRunning,
		"panel_running":    row.PanelRunning,
		"ping_ms":          row.PingMs,
		"tcp_connections":  row.TCPConnections,
		"udp_connections":  row.UDPConnections,
	}
	if metrics.PanelVersion != nil {
		assignments["panel_version"] = metrics.PanelVersion
	} else {
		assignments["panel_version"] = gorm.Expr("node_metrics_latest.panel_version")
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "node_id"}},
		DoUpdates: clause.Assignments(assignments),
	}).Create(&row).Error
}

func (s *Service) insertMetricSample(ctx context.Context, nodeID uuid.UUID, metrics NodeMetrics) error {
	if s == nil || s.DB == nil {
		return nil
	}
	memAvail := (*int64)(nil)
	if metrics.RAMTotalBytes != nil && metrics.RAMUsedBytes != nil {
		if *metrics.RAMTotalBytes >= *metrics.RAMUsedBytes {
			val := *metrics.RAMTotalBytes - *metrics.RAMUsedBytes
			memAvail = &val
		}
	}
	row := db.NodeMetric{
		NodeID:            nodeID,
		TS:                metrics.CollectedAt,
		MemTotalBytes:     metrics.RAMTotalBytes,
		MemAvailableBytes: memAvail,
		DiskTotalBytes:    metrics.DiskTotalBytes,
		DiskUsedBytes:     metrics.DiskUsedBytes,
		NetRxBytes:        metrics.NetRxBytes,
		NetTxBytes:        metrics.NetTxBytes,
		PingMs:            metrics.PingMs,
	}
	return s.DB.WithContext(ctx).Create(&row).Error
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
	s.applySources(nodes)
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
		Select(`n.id as node_id, n.name, n.kind, n.region, n.provider, n.is_enabled, n.is_sandbox,
			n.agent_installed, n.agent_last_seen_at, n.agent_version,
			m.collected_at, m.cpu_pct, m.ram_used_bytes, m.ram_total_bytes,
			m.disk_used_bytes, m.disk_total_bytes, m.net_rx_bps, m.net_tx_bps,
			m.net_rx_bytes, m.net_tx_bytes, m.net_iface, m.uptime_sec, m.panel_version,
			m.xray_running, m.panel_running, m.ping_ms, m.tcp_connections, m.udp_connections,
			NULL::text as last_error`).
		Joins("LEFT JOIN node_metrics_latest m ON m.node_id = n.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].AgentOnline = computeAgentOnline(rows[i].AgentLastSeenAt, rows[i].AgentInstalled, 90*time.Second)
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
	deduped := make(map[string]DashboardActiveUser, len(rows))
	for _, row := range rows {
		inbound := ""
		if row.InboundTag != nil {
			inbound = strings.TrimSpace(*row.InboundTag)
		}
		key := strings.ToLower(strings.TrimSpace(row.ClientEmail)) + "|" + inbound + "|" + strings.TrimSpace(row.IP)
		prev, ok := deduped[key]
		if !ok || row.LastSeen.After(prev.LastSeen) {
			deduped[key] = row
		}
	}
	out := make([]DashboardActiveUser, 0, len(deduped))
	for _, row := range deduped {
		out = append(out, row)
	}
	return out, nil
}

type DashboardNode struct {
	NodeID          uuid.UUID  `json:"node_id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	Region          string     `json:"region"`
	Provider        string     `json:"provider"`
	IsEnabled       bool       `json:"is_enabled"`
	IsSandbox       bool       `json:"is_sandbox"`
	AgentInstalled  bool       `json:"agent_installed"`
	AgentLastSeenAt *time.Time `json:"agent_last_seen_at"`
	AgentOnline     bool       `json:"agent_online"`
	AgentVersion    *string    `json:"agent_version"`
	CollectedAt     *time.Time `json:"collected_at"`
	CPUPct          *float64   `json:"cpu_pct"`
	RAMUsedBytes    *int64     `json:"ram_used_bytes"`
	RAMTotalBytes   *int64     `json:"ram_total_bytes"`
	DiskUsedBytes   *int64     `json:"disk_used_bytes"`
	DiskTotalBytes  *int64     `json:"disk_total_bytes"`
	NetRxBps        *int64     `json:"net_rx_bps"`
	NetTxBps        *int64     `json:"net_tx_bps"`
	NetRxBytes      *int64     `json:"net_rx_bytes"`
	NetTxBytes      *int64     `json:"net_tx_bytes"`
	UptimeSec       *int64     `json:"uptime_sec"`
	PanelVersion    *string    `json:"panel_version"`
	XrayRunning     *bool      `json:"xray_running"`
	PanelRunning    *bool      `json:"panel_running"`
	NetIface        *string    `json:"net_iface"`
	PingMs          *int64     `json:"ping_ms"`
	TCPConnections  *int64     `json:"tcp_connections"`
	UDPConnections  *int64     `json:"udp_connections"`
	LastError       *string    `json:"last_error"`
	UsersSource     string     `json:"active_users_source"`
	UsersDetail     string     `json:"active_users_source_detail"`
	UsersAvailable  bool       `json:"active_users_available"`
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
	s.applySources(nodes)
	agg := computeAggregate(nodes)
	traffic24h, traffic7d, err := s.computeTrafficTotals(ctx)
	if err == nil {
		agg.TotalTraffic24h = traffic24h
		agg.TotalTraffic7d = traffic7d
	}
	agg.ActiveAlertsCount = s.countActiveAlerts(ctx)
	agg.ActiveUsers = s.countActiveUsers(ctx)
	return map[string]any{
		"nodes":        nodes,
		"aggregate":    agg,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) ActiveUsers(ctx context.Context, limit int, search string) ([]DashboardActiveUser, error) {
	return s.listActiveUsers(ctx, limit, search)
}

type AggregateSummary struct {
	NodesTotal        int      `json:"nodes_total"`
	NodesOnline       int      `json:"nodes_online"`
	NodesOffline      int      `json:"nodes_offline"`
	AgentsActive      int      `json:"agents_active"`
	AgentsTotal       int      `json:"agents_total"`
	PanelsAvailable   int      `json:"panels_available"`
	AvgCPU            float64  `json:"avg_cpu"`
	AvgPingMs         *float64 `json:"avg_ping_ms"`
	TotalRxBps        int64    `json:"total_rx_bps"`
	TotalTxBps        int64    `json:"total_tx_bps"`
	TotalTraffic24h   *int64   `json:"total_traffic_24h"`
	TotalTraffic7d    *int64   `json:"total_traffic_7d"`
	TotalConnections  *int64   `json:"total_connections"`
	ActiveUsers       int      `json:"active_users"`
	ActiveAlertsCount int      `json:"active_alerts"`
}

func computeAggregate(nodes []DashboardNode) AggregateSummary {
	var agg AggregateSummary
	var cpuCount int
	var cpuSum float64
	var pingCount int
	var pingSum float64
	var totalConnections int64
	for _, node := range nodes {
		agg.NodesTotal++
		if node.AgentInstalled {
			agg.AgentsTotal++
		}
		if node.AgentOnline {
			agg.NodesOnline++
			agg.AgentsActive++
		}
		if node.PanelRunning != nil && *node.PanelRunning {
			agg.PanelsAvailable++
		}
		if node.CPUPct != nil {
			cpuCount++
			cpuSum += *node.CPUPct
		}
		if node.AgentOnline && node.PingMs != nil {
			pingCount++
			pingSum += float64(*node.PingMs)
		}
		if node.NetRxBps != nil {
			agg.TotalRxBps += *node.NetRxBps
		}
		if node.NetTxBps != nil {
			agg.TotalTxBps += *node.NetTxBps
		}
		if node.TCPConnections != nil {
			totalConnections += *node.TCPConnections
		}
		if node.UDPConnections != nil {
			totalConnections += *node.UDPConnections
		}
	}
	if cpuCount > 0 {
		agg.AvgCPU = cpuSum / float64(cpuCount)
	}
	if pingCount > 0 {
		val := pingSum / float64(pingCount)
		agg.AvgPingMs = &val
	}
	if totalConnections > 0 {
		agg.TotalConnections = &totalConnections
	}
	agg.NodesOffline = agg.NodesTotal - agg.NodesOnline
	return agg
}

type trafficPoint struct {
	NodeID    uuid.UUID
	TS        time.Time
	NetRx     *int64
	NetTx     *int64
}

func (s *Service) computeTrafficTotals(ctx context.Context) (*int64, *int64, error) {
	traffic24h, err := s.computeTrafficTotalForRange(ctx, 24*time.Hour)
	if err != nil {
		return nil, nil, err
	}
	traffic7d, err := s.computeTrafficTotalForRange(ctx, 7*24*time.Hour)
	if err != nil {
		return nil, nil, err
	}
	return traffic24h, traffic7d, nil
}

func (s *Service) computeTrafficTotalForRange(ctx context.Context, window time.Duration) (*int64, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	cutoff := time.Now().UTC().Add(-window)
	var rows []trafficPoint
	err := s.DB.WithContext(ctx).
		Table("node_metrics").
		Select("node_id, ts, net_rx_bytes as net_rx, net_tx_bytes as net_tx").
		Where("ts >= ?", cutoff).
		Where("net_rx_bytes IS NOT NULL AND net_tx_bytes IS NOT NULL").
		Order("node_id, ts").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	var total int64
	var current uuid.UUID
	var prevRx, prevTx *int64
	first := true
	for _, row := range rows {
		if first || row.NodeID != current {
			current = row.NodeID
			prevRx, prevTx = row.NetRx, row.NetTx
			first = false
			continue
		}
		if prevRx != nil && row.NetRx != nil {
			if delta := *row.NetRx - *prevRx; delta > 0 {
				total += delta
			}
		}
		if prevTx != nil && row.NetTx != nil {
			if delta := *row.NetTx - *prevTx; delta > 0 {
				total += delta
			}
		}
		prevRx, prevTx = row.NetRx, row.NetTx
	}
	return &total, nil
}

func (s *Service) countActiveAlerts(ctx context.Context) int {
	if s == nil || s.DB == nil {
		return 0
	}
	var count int64
	now := time.Now().UTC()
	err := s.DB.WithContext(ctx).
		Table("alert_states").
		Where("last_status IN ('fail','warn')").
		Where("muted_until IS NULL OR muted_until < ?", now).
		Count(&count).Error
	if err != nil {
		return 0
	}
	return int(count)
}

func (s *Service) countActiveUsers(ctx context.Context) int {
	if s == nil || s.DB == nil {
		return 0
	}
	var count int64
	if err := s.DB.WithContext(ctx).Table("active_users_latest").Count(&count).Error; err != nil {
		return 0
	}
	return int(count)
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
		"net_iface":        metrics.NetIface,
		"uptime_sec":       metrics.UptimeSec,
		"agent_version":    metrics.AgentVersion,
		"panel_version":    metrics.PanelVersion,
		"xray_running":     metrics.XrayRunning,
		"panel_running":    metrics.PanelRunning,
		"ping_ms":          metrics.PingMs,
		"tcp_connections":  metrics.TCPConnections,
		"udp_connections":  metrics.UDPConnections,
	}
}

type ActiveUsersSource struct {
	Source       string
	SourceDetail string
	Available    bool
	UpdatedAt    time.Time
}

func (s *Service) setActiveUsersSource(nodeID uuid.UUID, source, detail string, available bool) {
	if s == nil {
		return
	}
	s.sourceMu.Lock()
	defer s.sourceMu.Unlock()
	s.sources[nodeID] = ActiveUsersSource{
		Source:       source,
		SourceDetail: detail,
		Available:    available,
		UpdatedAt:    time.Now(),
	}
}

func (s *Service) applySources(nodes []DashboardNode) {
	if s == nil {
		return
	}
	s.sourceMu.RLock()
	defer s.sourceMu.RUnlock()
	for i := range nodes {
		if src, ok := s.sources[nodes[i].NodeID]; ok {
			nodes[i].UsersSource = src.Source
			nodes[i].UsersDetail = src.SourceDetail
			nodes[i].UsersAvailable = src.Available
			continue
		}
		nodes[i].UsersSource = "unknown"
		nodes[i].UsersDetail = "not collected yet"
		nodes[i].UsersAvailable = false
	}
}
