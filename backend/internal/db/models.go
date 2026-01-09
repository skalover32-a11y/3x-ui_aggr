package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
)

type Node struct {
	ID                uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name              string         `gorm:"type:text;not null" json:"name"`
	Kind              string         `gorm:"type:text;not null;default:'PANEL'" json:"kind"`
	Tags              pq.StringArray `gorm:"type:text[]" json:"tags"`
	Host              string         `gorm:"type:text" json:"host"`
	Region            string         `gorm:"type:text" json:"region"`
	Provider          string         `gorm:"type:text" json:"provider"`
	BaseURL           string         `gorm:"type:text;not null" json:"base_url"`
	PanelUsername     string         `gorm:"type:text;not null" json:"panel_username"`
	PanelPasswordEnc  string         `gorm:"type:text;not null" json:"-"`
	Capabilities      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"capabilities"`
	AllowedRoots      pq.StringArray `gorm:"type:text[]" json:"allowed_roots"`
	IsSandbox         bool           `gorm:"not null;default:false" json:"is_sandbox"`
	AgentEnabled      bool           `gorm:"not null;default:false" json:"agent_enabled"`
	AgentURL          *string        `gorm:"type:text" json:"agent_url"`
	AgentTokenEnc     *string        `gorm:"type:text" json:"-"`
	AgentInsecureTLS  bool           `gorm:"column:agent_allow_insecure_tls;not null;default:false" json:"agent_allow_insecure_tls"`
	IsEnabled         bool           `gorm:"not null;default:true" json:"is_enabled"`
	SSHEnabled        bool           `gorm:"not null;default:true" json:"ssh_enabled"`
	SSHHost           string         `gorm:"type:text;not null" json:"ssh_host"`
	SSHPort           int            `gorm:"type:int;not null" json:"ssh_port"`
	SSHUser           string         `gorm:"type:text;not null" json:"ssh_user"`
	SSHAuthMethod     string         `gorm:"type:text;not null;default:'key'" json:"ssh_auth_method"`
	SSHPasswordEnc    *string        `gorm:"type:text" json:"-"`
	SSHKeyEnc         string         `gorm:"type:text;not null" json:"-"`
	VerifyTLS         bool           `gorm:"not null;default:true" json:"verify_tls"`
	XrayVersion       *string        `gorm:"type:text" json:"xray_version"`
	PanelVersion      *string        `gorm:"type:text" json:"panel_version"`
	VersionsCheckedAt *time.Time     `json:"versions_checked_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type Service struct {
	ID             uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID         uuid.UUID      `gorm:"type:uuid;not null" json:"node_id"`
	Kind           string         `gorm:"type:text;not null" json:"kind"`
	URL            *string        `gorm:"type:text" json:"url"`
	Host           *string        `gorm:"type:text" json:"host"`
	Port           *int           `gorm:"type:int" json:"port"`
	TLSMode        *string        `gorm:"type:text" json:"tls_mode"`
	HealthPath     *string        `gorm:"type:text" json:"health_path"`
	ExpectedStatus pq.Int64Array  `gorm:"type:int[]" json:"expected_status"`
	Headers        datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"headers"`
	AuthRef        *string        `gorm:"type:text" json:"auth_ref"`
	IsEnabled      bool           `gorm:"not null;default:true" json:"is_enabled"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type Bot struct {
	ID              uuid.UUID     `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID          uuid.UUID     `gorm:"type:uuid;not null" json:"node_id"`
	Name            string        `gorm:"type:text;not null" json:"name"`
	Kind            string        `gorm:"type:text;not null" json:"kind"`
	DockerContainer *string       `gorm:"type:text" json:"docker_container"`
	SystemdUnit     *string       `gorm:"type:text" json:"systemd_unit"`
	HealthURL       *string       `gorm:"type:text" json:"health_url"`
	HealthPath      *string       `gorm:"type:text;default:'/'" json:"health_path"`
	ExpectedStatus  pq.Int64Array `gorm:"type:int[];not null;default:'{200}'" json:"expected_status"`
	IsEnabled       bool          `gorm:"not null;default:true" json:"is_enabled"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type Check struct {
	ID            uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	TargetType    string         `gorm:"type:text;not null" json:"target_type"`
	TargetID      uuid.UUID      `gorm:"type:uuid;not null" json:"target_id"`
	Type          string         `gorm:"type:text;not null" json:"type"`
	IntervalSec   int            `gorm:"not null;default:60" json:"interval_sec"`
	TimeoutMS     int            `gorm:"not null;default:3000" json:"timeout_ms"`
	Retries       int            `gorm:"not null;default:1" json:"retries"`
	Enabled       bool           `gorm:"not null;default:true" json:"enabled"`
	SeverityRules datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"severity_rules"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type CheckResult struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	CheckID   uuid.UUID      `gorm:"type:uuid;not null" json:"check_id"`
	TS        time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	Status    string         `gorm:"type:text;not null" json:"status"`
	Metrics   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"metrics"`
	Error     *string        `gorm:"type:text" json:"error"`
	LatencyMS *int           `gorm:"type:int" json:"latency_ms"`
}

type AlertState struct {
	Fingerprint    string         `gorm:"type:text;primaryKey" json:"fingerprint"`
	AlertType      string         `gorm:"type:text;not null" json:"alert_type"`
	NodeID         *uuid.UUID     `gorm:"type:uuid" json:"node_id"`
	ServiceID      *uuid.UUID     `gorm:"type:uuid" json:"service_id"`
	BotID          *uuid.UUID     `gorm:"type:uuid" json:"bot_id"`
	CheckType      *string        `gorm:"type:text" json:"check_type"`
	LastStatus     *string        `gorm:"type:text" json:"last_status"`
	FirstSeen      time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"first_seen"`
	LastSeen       time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"last_seen"`
	Occurrences    int            `gorm:"not null;default:1" json:"occurrences"`
	LastMessageIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'::jsonb" json:"last_message_ids"`
	MutedUntil     *time.Time     `gorm:"type:timestamptz" json:"muted_until"`
	UpdatedAt      time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
}
type AuditLog struct {
	ID          uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	TS          time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	Actor       string         `gorm:"type:text;not null" json:"actor"`
	ActorUser   *string        `gorm:"type:text" json:"actor_user"`
	IP          *string        `gorm:"type:text" json:"ip"`
	NodeID      *uuid.UUID     `gorm:"type:uuid" json:"node_id"`
	Action      string         `gorm:"type:text;not null" json:"action"`
	Status      string         `gorm:"type:text;not null" json:"status"`
	Message     *string        `gorm:"type:text" json:"message"`
	Payload     datatypes.JSON `gorm:"type:jsonb;not null" json:"payload"`
	PayloadJSON datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"payload_json"`
	Error       *string        `gorm:"type:text" json:"error"`
	CreatedAt   time.Time      `json:"created_at"`
}

type NodeCheck struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID    uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	TS        time.Time `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	PanelOK   bool      `gorm:"not null" json:"panel_ok"`
	SSHOK     bool      `gorm:"not null" json:"ssh_ok"`
	LatencyMS int       `gorm:"type:int" json:"latency_ms"`
	Error     *string   `gorm:"type:text" json:"error"`
}

type NodeMetric struct {
	ID                uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID            uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	TS                time.Time `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	Load1             *float64  `json:"load1"`
	Load5             *float64  `json:"load5"`
	Load15            *float64  `json:"load15"`
	MemTotalBytes     *int64    `json:"mem_total_bytes"`
	MemAvailableBytes *int64    `json:"mem_available_bytes"`
	DiskTotalBytes    *int64    `json:"disk_total_bytes"`
	DiskUsedBytes     *int64    `json:"disk_used_bytes"`
	Error             *string   `json:"error"`
}

type NodeMetricsLatest struct {
	NodeID         uuid.UUID `gorm:"type:uuid;primaryKey" json:"node_id"`
	CollectedAt    time.Time `gorm:"type:timestamptz;not null" json:"collected_at"`
	CPUPct         *float64  `json:"cpu_pct"`
	RAMUsedBytes   *int64    `json:"ram_used_bytes"`
	RAMTotalBytes  *int64    `json:"ram_total_bytes"`
	DiskUsedBytes  *int64    `json:"disk_used_bytes"`
	DiskTotalBytes *int64    `json:"disk_total_bytes"`
	NetRxBps       *int64    `json:"net_rx_bps"`
	NetTxBps       *int64    `json:"net_tx_bps"`
	NetRxBytes     *int64    `json:"net_rx_bytes"`
	NetTxBytes     *int64    `json:"net_tx_bytes"`
	NetIface       *string   `json:"net_iface"`
	UptimeSec      *int64    `json:"uptime_sec"`
	PanelVersion   *string   `json:"panel_version"`
	XrayRunning    *bool     `json:"xray_running"`
	PanelRunning   *bool     `json:"panel_running"`
}

type ActiveUserLatest struct {
	ID             uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID         uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	InboundTag     *string   `gorm:"type:text" json:"inbound_tag"`
	ClientEmail    string    `gorm:"type:text;not null" json:"client_email"`
	IP             string    `gorm:"type:text;not null;default:''" json:"ip"`
	RxBps          *int64    `json:"rx_bps"`
	TxBps          *int64    `json:"tx_bps"`
	TotalUpBytes   *int64    `json:"total_up_bytes"`
	TotalDownBytes *int64    `json:"total_down_bytes"`
	LastSeen       time.Time `gorm:"type:timestamptz;not null" json:"last_seen"`
	CollectedAt    time.Time `gorm:"type:timestamptz;not null" json:"collected_at"`
}

type TelegramSettings struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	BotTokenEnc     string    `gorm:"type:text;not null" json:"-"`
	AdminChatID     string    `gorm:"type:text;not null" json:"admin_chat_id"`
	AlertConnection bool      `gorm:"not null;default:true" json:"alert_connection"`
	AlertCPU        bool      `gorm:"not null;default:true" json:"alert_cpu"`
	AlertMemory     bool      `gorm:"not null;default:true" json:"alert_memory"`
	AlertDisk       bool      `gorm:"not null;default:true" json:"alert_disk"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type User struct {
	ID           uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Username     string     `gorm:"type:text;not null" json:"username"`
	PasswordHash string     `gorm:"type:text;not null" json:"-"`
	Role         string     `gorm:"type:text;not null" json:"role"`
	TOTPSecret   *string    `gorm:"column:totp_secret_enc;type:text" json:"-"`
	TOTPEnabled  bool       `gorm:"not null;default:false" json:"totp_enabled"`
	RecoveryHash *string    `gorm:"column:recovery_code_hash;type:text" json:"-"`
	RecoveryExp  *time.Time `gorm:"column:recovery_code_expires_at;type:timestamptz" json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type WebAuthnCredential struct {
	ID           uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID       string         `gorm:"type:text;not null" json:"user_id"`
	CredentialID string         `gorm:"type:text;not null;unique" json:"credential_id"`
	PublicKey    []byte         `gorm:"type:bytea;not null" json:"-"`
	SignCount    int64          `gorm:"not null;default:0" json:"sign_count"`
	Transports   pq.StringArray `gorm:"type:text[];not null;default:'{}'" json:"transports"`
	AAGUID       *string        `gorm:"column:aaguid;type:text" json:"aaguid"`
	CreatedAt    time.Time      `json:"created_at"`
	LastUsedAt   *time.Time     `gorm:"type:timestamptz" json:"last_used_at"`
}

func (WebAuthnCredential) TableName() string {
	return "webauthn_credentials"
}

type WebAuthnChallenge struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    string         `gorm:"type:text;not null" json:"user_id"`
	Type      string         `gorm:"type:text;not null" json:"type"`
	Challenge string         `gorm:"type:text;not null" json:"challenge"`
	Session   datatypes.JSON `gorm:"column:session_data;type:jsonb;not null;default:'{}'::jsonb" json:"session"`
	Options   datatypes.JSON `gorm:"column:options_data;type:jsonb;not null;default:'{}'::jsonb" json:"options"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `gorm:"type:timestamptz;not null" json:"expires_at"`
}

func (WebAuthnChallenge) TableName() string {
	return "webauthn_challenges"
}

type RefreshToken struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID     string     `gorm:"type:text;not null" json:"user_id"`
	TokenHash  string     `gorm:"type:text;not null;unique" json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `gorm:"type:timestamptz;not null" json:"expires_at"`
	LastUsedAt *time.Time `gorm:"type:timestamptz" json:"last_used_at"`
	RevokedAt  *time.Time `gorm:"type:timestamptz" json:"revoked_at"`
	UserAgent  *string    `gorm:"type:text" json:"user_agent"`
	IP         *string    `gorm:"type:text" json:"ip"`
	DeviceName *string    `gorm:"type:text" json:"device_name"`
}

func (RefreshToken) TableName() string {
	return "auth_refresh_tokens"
}

type OpsJob struct {
	ID              uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Type            string         `gorm:"type:text;not null" json:"type"`
	Status          string         `gorm:"type:text;not null" json:"status"`
	CreatedByActor  string         `gorm:"type:text;not null" json:"created_by_actor"`
	CreatedByUserID *uuid.UUID     `gorm:"type:uuid" json:"created_by_user_id"`
	Parallelism     int            `gorm:"not null;default:5" json:"parallelism"`
	Targets         datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'::jsonb" json:"targets"`
	Params          datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"params"`
	Error           *string        `gorm:"type:text" json:"error"`
	CreatedAt       time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	StartedAt       *time.Time     `gorm:"type:timestamptz" json:"started_at"`
	FinishedAt      *time.Time     `gorm:"type:timestamptz" json:"finished_at"`
	Summary         *OpsJobSummary `gorm:"-" json:"summary,omitempty"`
}

type OpsJobSummary struct {
	Total   int `json:"total"`
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

type OpsJobItem struct {
	ID         uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	JobID      uuid.UUID  `gorm:"type:uuid;not null" json:"job_id"`
	NodeID     uuid.UUID  `gorm:"type:uuid;not null" json:"node_id"`
	Status     string     `gorm:"type:text;not null" json:"status"`
	Log        string     `gorm:"type:text;not null;default:''" json:"log"`
	ExitCode   *int       `gorm:"type:int" json:"exit_code"`
	Error      *string    `gorm:"type:text" json:"error"`
	StartedAt  *time.Time `gorm:"type:timestamptz" json:"started_at"`
	FinishedAt *time.Time `gorm:"type:timestamptz" json:"finished_at"`
}
