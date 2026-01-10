package dashboard

import "time"

type NodeMetrics struct {
	CollectedAt    time.Time
	FromAgent      bool
	CPUPct         *float64
	RAMUsedBytes   *int64
	RAMTotalBytes  *int64
	DiskUsedBytes  *int64
	DiskTotalBytes *int64
	NetRxBps       *int64
	NetTxBps       *int64
	NetRxBytes     *int64
	NetTxBytes     *int64
	NetIface       *string
	UptimeSec      *int64
	AgentVersion   *string
	PanelVersion   *string
	XrayRunning    *bool
	PanelRunning   *bool
}

type ActiveUser struct {
	InboundTag     *string
	ClientEmail    string
	IP             string
	RxBps          *int64
	TxBps          *int64
	TotalUpBytes   *int64
	TotalDownBytes *int64
	LastSeen       time.Time
}

type ActiveUsersResult struct {
	Users        []ActiveUser
	Source       string
	SourceDetail string
	Available    bool
}
