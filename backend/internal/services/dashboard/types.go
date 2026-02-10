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
	PingMs         *int64
	TCPConnections *int64
	UDPConnections *int64
}

type ActiveUser struct {
	ClientEmail    string    `json:"client_email"`
	IP             string    `json:"ip"`
	RxBps          *int64    `json:"rx_bps"`
	TxBps          *int64    `json:"tx_bps"`
	TotalUpBytes   *int64    `json:"total_up_bytes"`
	TotalDownBytes *int64    `json:"total_down_bytes"`
	LastSeen       time.Time `json:"last_seen"`
}

type ActiveUsersResult struct {
	Users        []ActiveUser
	Source       string
	SourceDetail string
	Available    bool
}
