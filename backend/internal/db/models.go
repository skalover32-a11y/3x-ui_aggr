package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
)

type Node struct {
	ID                uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OrgID             *uuid.UUID     `gorm:"type:uuid" json:"org_id,omitempty"`
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
	AgentInstalled    bool           `gorm:"not null;default:false" json:"agent_installed"`
	AgentVersion      *string        `gorm:"type:text" json:"agent_version"`
	AgentLastSeenAt   *time.Time     `gorm:"type:timestamptz" json:"agent_last_seen_at"`
	IsEnabled         bool           `gorm:"not null;default:true" json:"is_enabled"`
	SSHEnabled        bool           `gorm:"not null;default:true" json:"ssh_enabled"`
	SSHHost           string         `gorm:"type:text;not null" json:"ssh_host"`
	SSHPort           int            `gorm:"type:int;not null" json:"ssh_port"`
	SSHUser           string         `gorm:"type:text;not null" json:"ssh_user"`
	SSHAuthMethod     string         `gorm:"type:text;not null;default:'key'" json:"ssh_auth_method"`
	SSHPasswordEnc    *string        `gorm:"type:text" json:"-"`
	SSHKeyEnc         string         `gorm:"type:text;not null" json:"-"`
	VerifyTLS         bool           `gorm:"not null;default:true" json:"verify_tls"`
	RuntimeVersion       *string        `gorm:"type:text" json:"runtime_version"`
	PanelVersion      *string        `gorm:"type:text" json:"service_version"`
	VersionsCheckedAt *time.Time     `json:"versions_checked_at"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type Organization struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string    `gorm:"type:text;not null" json:"name"`
	OwnerUserID uuid.UUID `gorm:"type:uuid;not null" json:"owner_user_id"`
	CreatedAt   time.Time `json:"created_at"`
}

type OrganizationMember struct {
	OrgID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"org_id"`
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	Role      string    `gorm:"type:org_role;not null" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentCredential struct {
	NodeID     uuid.UUID  `gorm:"type:uuid;primaryKey" json:"node_id"`
	TokenHash  string     `gorm:"type:text;not null" json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

type NodeRegistrationToken struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID    uuid.UUID  `gorm:"type:uuid;not null" json:"node_id"`
	TokenHash string     `gorm:"type:text;not null" json:"-"`
	ExpiresAt time.Time  `gorm:"type:timestamptz;not null" json:"expires_at"`
	UsedAt    *time.Time `gorm:"type:timestamptz" json:"used_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type Invite struct {
	ID              uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Code            string     `gorm:"type:text;unique;not null" json:"code"`
	CreatedByUserID uuid.UUID  `gorm:"type:uuid;not null" json:"created_by_user_id"`
	TargetOrgID     *uuid.UUID `gorm:"type:uuid" json:"target_org_id,omitempty"`
	Mode            string     `gorm:"type:text;not null" json:"mode"`
	Role            string     `gorm:"type:org_role;not null;default:'owner'" json:"role"`
	OrgName         *string    `gorm:"type:text" json:"org_name,omitempty"`
	ExpiresAt       time.Time  `gorm:"type:timestamptz;not null" json:"expires_at"`
	UsedAt          *time.Time `gorm:"type:timestamptz" json:"used_at,omitempty"`
	UsedByUserID    *uuid.UUID `gorm:"type:uuid" json:"used_by_user_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
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
	FailAfterSec  int            `gorm:"not null;default:300" json:"fail_after_sec"`
	RecoverAfterOK int           `gorm:"not null;default:2" json:"recover_after_ok"`
	MuteUntil     *time.Time     `gorm:"type:timestamptz" json:"mute_until,omitempty"`
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
	AlertID        *uuid.UUID     `gorm:"type:uuid;uniqueIndex" json:"alert_id"`
	Fingerprint    string         `gorm:"type:text;primaryKey" json:"fingerprint"`
	IncidentID     *uuid.UUID     `gorm:"type:uuid" json:"incident_id,omitempty"`
	AlertType      string         `gorm:"type:text;not null" json:"alert_type"`
	NodeID         *uuid.UUID     `gorm:"type:uuid" json:"node_id"`
	ServiceID      *uuid.UUID     `gorm:"type:uuid" json:"service_id"`
	BotID          *uuid.UUID     `gorm:"type:uuid" json:"bot_id"`
	CheckType      *string        `gorm:"type:text" json:"check_type"`
	LastStatus     *string        `gorm:"type:text" json:"last_status"`
	FirstSeen      time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"first_seen"`
	LastSeen       time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"last_seen"`
	Occurrences    int            `gorm:"not null;default:1" json:"occurrences"`
	OKStreak       int            `gorm:"not null;default:0" json:"ok_streak"`
	LastMessageIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'::jsonb" json:"last_message_ids"`
	MutedUntil     *time.Time     `gorm:"type:timestamptz" json:"muted_until"`
	UpdatedAt      time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
}

type Incident struct {
	ID             uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OrgID          *uuid.UUID `gorm:"type:uuid" json:"org_id,omitempty"`
	Fingerprint    string     `gorm:"type:text;uniqueIndex;not null" json:"fingerprint"`
	AlertType      string     `gorm:"type:text;not null" json:"alert_type"`
	Severity       string     `gorm:"type:text;not null;default:'critical'" json:"severity"`
	Status         string     `gorm:"type:text;not null;default:'open'" json:"status"`
	NodeID         *uuid.UUID `gorm:"type:uuid" json:"node_id,omitempty"`
	ServiceID      *uuid.UUID `gorm:"type:uuid" json:"service_id,omitempty"`
	BotID          *uuid.UUID `gorm:"type:uuid" json:"bot_id,omitempty"`
	CheckID        *uuid.UUID `gorm:"type:uuid" json:"check_id,omitempty"`
	Title          string     `gorm:"type:text;not null;default:''" json:"title"`
	Description    *string    `gorm:"type:text" json:"description,omitempty"`
	FirstSeen      time.Time  `gorm:"type:timestamptz;not null;default:now()" json:"first_seen"`
	LastSeen       time.Time  `gorm:"type:timestamptz;not null;default:now()" json:"last_seen"`
	AcknowledgedAt *time.Time `gorm:"type:timestamptz" json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `gorm:"type:text" json:"acknowledged_by,omitempty"`
	RecoveredAt    *time.Time `gorm:"type:timestamptz" json:"recovered_at,omitempty"`
	Occurrences    int        `gorm:"not null;default:1" json:"occurrences"`
	LastError      *string    `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt      time.Time  `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"type:timestamptz;not null;default:now()" json:"updated_at"`
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
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID           uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	TS               time.Time `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	PanelOK          bool      `gorm:"not null" json:"panel_ok"`
	SSHOK            bool      `gorm:"not null" json:"ssh_ok"`
	LatencyMS        int       `gorm:"type:int" json:"latency_ms"`
	Error            *string   `gorm:"type:text" json:"error"`
	PanelErrorCode   *string   `gorm:"type:text" json:"panel_error_code"`
	PanelErrorDetail *string   `gorm:"type:text" json:"panel_error_detail"`
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
	NetRxBytes        *int64    `json:"net_rx_bytes"`
	NetTxBytes        *int64    `json:"net_tx_bytes"`
	PingMs            *int64    `json:"ping_ms"`
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
	PanelVersion   *string   `json:"service_version"`
	RuntimeRunning    *bool     `json:"runtime_running"`
	PanelRunning   *bool     `json:"service_running"`
	PingMs         *int64    `json:"ping_ms"`
	TCPConnections *int64    `json:"tcp_connections"`
	UDPConnections *int64    `json:"udp_connections"`
}

func (NodeMetricsLatest) TableName() string {
	return "node_metrics_latest"
}

type ActiveUserLatest struct {
	ID             uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	NodeID         uuid.UUID `gorm:"type:uuid;not null" json:"node_id"`
	SourceTag      *string   `gorm:"type:text" json:"source_tag"`
	ClientEmail    string    `gorm:"type:text;not null" json:"client_email"`
	IP             string    `gorm:"type:text;not null;default:''" json:"ip"`
	RxBps          *int64    `json:"rx_bps"`
	TxBps          *int64    `json:"tx_bps"`
	TotalUpBytes   *int64    `json:"total_up_bytes"`
	TotalDownBytes *int64    `json:"total_down_bytes"`
	LastSeen       time.Time `gorm:"type:timestamptz;not null" json:"last_seen"`
	CollectedAt    time.Time `gorm:"type:timestamptz;not null" json:"collected_at"`
}

func (ActiveUserLatest) TableName() string {
	return "active_users_latest"
}

type TelegramSettings struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OrgID           *uuid.UUID `gorm:"type:uuid;index" json:"org_id,omitempty"`
	BotTokenEnc     string    `gorm:"type:text;not null" json:"-"`
	AdminChatID     string    `gorm:"type:text;not null" json:"admin_chat_id"`
	AlertConnection bool      `gorm:"not null;default:true" json:"alert_connection"`
	AlertCPU        bool      `gorm:"not null;default:true" json:"alert_cpu"`
	AlertMemory     bool      `gorm:"not null;default:true" json:"alert_memory"`
	AlertDisk       bool      `gorm:"not null;default:true" json:"alert_disk"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type OrgKey struct {
	ID            uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OrgID         uuid.UUID  `gorm:"type:uuid;not null;index:idx_org_keys_org_id" json:"org_id"`
	Filename      string     `gorm:"type:text;not null" json:"filename"`
	Ext           string     `gorm:"type:text;not null" json:"ext"`
	ContentEnc    string     `gorm:"type:text;not null" json:"-"`
	SizeBytes     int        `gorm:"not null;default:0" json:"size_bytes"`
	Label         *string    `gorm:"type:text" json:"label,omitempty"`
	Description   *string    `gorm:"type:text" json:"description,omitempty"`
	Fingerprint   *string    `gorm:"type:text" json:"fingerprint,omitempty"`
	NodeID        *uuid.UUID `gorm:"type:uuid" json:"node_id,omitempty"`
	CreatedByUser *uuid.UUID `gorm:"column:created_by_user_id;type:uuid" json:"created_by_user_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (OrgKey) TableName() string {
	return "org_keys"
}

type FSAuditLog struct {
	ID     uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	TS     time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"ts"`
	UserID *uuid.UUID     `gorm:"type:uuid" json:"user_id"`
	Actor  string         `gorm:"type:text;not null" json:"actor"`
	NodeID uuid.UUID      `gorm:"type:uuid;not null" json:"node_id"`
	Op     string         `gorm:"type:text;not null" json:"op"`
	Path   string         `gorm:"type:text;not null" json:"path"`
	Extra  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'::jsonb" json:"extra"`
	OK     bool           `gorm:"not null;default:true" json:"ok"`
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
	PublicTokenHash *string        `gorm:"type:text" json:"-"`
	Error           *string        `gorm:"type:text" json:"error"`
	CreatedAt       time.Time      `gorm:"type:timestamptz;not null;default:now()" json:"created_at"`
	StartedAt       *time.Time     `gorm:"type:timestamptz" json:"started_at"`
	FinishedAt      *time.Time     `gorm:"type:timestamptz" json:"finished_at"`
	Summary         *OpsJobSummary `gorm:"-" json:"summary,omitempty"`
	PublicToken     *string        `gorm:"-" json:"public_token,omitempty"`
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

