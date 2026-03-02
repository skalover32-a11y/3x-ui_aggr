package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

type SSHExecutor struct {
	Encryptor *security.Encryptor
	Timeout   time.Duration
}

const (
	remnaCronFilePath      = "/etc/cron.d/remnanode-geodata"
	remnaLogrotateFilePath = "/etc/logrotate.d/remnanode-geodata"
)

func NewSSHExecutor(enc *security.Encryptor, timeout time.Duration) *SSHExecutor {
	return &SSHExecutor{Encryptor: enc, Timeout: timeout}
}

func (e *SSHExecutor) Reboot(ctx context.Context, node *db.Node) (string, int, error) {
	return e.runCommand(ctx, node, "sudo /sbin/reboot", true)
}

func (e *SSHExecutor) Update(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	if params.PrecheckOnly {
		return e.runUpdatePrecheck(ctx, node, params)
	}
	if _, code, err := e.runCommand(ctx, node, "command -v service-manager", false); err != nil {
		if code == 0 {
			code = 10
		}
		return "service-manager not installed", code, fmt.Errorf("service-manager not installed")
	}
	if _, code, err := e.runCommand(ctx, node, "command -v expect", false); err != nil {
		if params.InstallExpect {
			if _, _, err := e.runCommand(ctx, node, "sudo apt-get update && sudo apt-get install -y expect", false); err != nil {
				return "failed to install expect", 11, err
			}
		} else {
			if code == 0 {
				code = 11
			}
			return "expect not installed", code, fmt.Errorf("expect not installed")
		}
	}
	if _, code, err := e.runCommand(ctx, node, "command -v expect", false); err != nil {
		if code == 0 {
			code = 11
		}
		return "expect not installed", code, fmt.Errorf("expect not installed")
	}
	cmd := buildservicemgrUpdateCommand()
	return e.runCommand(ctx, node, cmd, false)
}

func (e *SSHExecutor) DeployAgent(ctx context.Context, node *db.Node, params DeployAgentParams) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	client, err := e.openClient(node)
	if err != nil {
		return "", 1, err
	}
	defer client.Close()

	logs := &strings.Builder{}
	writeLog(logs, "preflight ok")

	sudoPass, usePass, err := detectSudo(ctx, client, params.SudoPasswords)
	if err != nil {
		writeLog(logs, "sudo check failed")
		return logs.String(), 2, err
	}

	if _, _, err := runRemote(ctx, client, "command -v docker"); err != nil {
		if params.InstallDocker {
			writeLog(logs, "docker missing: installing")
			var lastErr error
			for attempt := 1; attempt <= 3; attempt++ {
				out, _, err := runRemote(ctx, client, sudoCmd("DEBIAN_FRONTEND=noninteractive apt-get update", sudoPass, usePass))
				if err != nil && isAptLockError(out) {
					writeLog(logs, fmt.Sprintf("apt lock detected (update), retry %d/3", attempt))
					time.Sleep(5 * time.Second)
					lastErr = err
					continue
				}
				if err != nil {
					writeLog(logs, "apt update failed: "+strings.TrimSpace(out))
					return logs.String(), 15, err
				}
				out, _, err = runRemote(ctx, client, sudoCmd("DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io", sudoPass, usePass))
				if err != nil && isAptLockError(out) {
					writeLog(logs, fmt.Sprintf("apt lock detected (install), retry %d/3", attempt))
					time.Sleep(5 * time.Second)
					lastErr = err
					continue
				}
				if err != nil {
					writeLog(logs, "docker install failed: "+strings.TrimSpace(out))
					return logs.String(), 15, err
				}
				lastErr = nil
				break
			}
			if lastErr != nil {
				return logs.String(), 15, lastErr
			}
			if _, _, err := runRemote(ctx, client, sudoCmd("systemctl enable --now docker", sudoPass, usePass)); err != nil {
				writeLog(logs, "docker enable failed")
				return logs.String(), 16, err
			}
			writeLog(logs, "docker installed")
		} else {
			writeLog(logs, "docker missing (not installed)")
		}
	} else {
		writeLog(logs, "docker ok")
	}

	if params.BinaryPath == "" {
		return logs.String(), 3, errors.New("binary path missing")
	}
	if err := uploadFile(ctx, client, params.BinaryPath, "/tmp/vlf-agent"); err != nil {
		writeLog(logs, "upload agent failed")
		return logs.String(), 4, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 755 /tmp/vlf-agent /usr/local/bin/vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "install agent binary failed")
		return logs.String(), 5, err
	}
	writeLog(logs, "agent binary installed")

	if err := uploadBytes(ctx, client, params.ConfigContent, "/tmp/vlf-agent.yaml"); err != nil {
		writeLog(logs, "upload config failed")
		return logs.String(), 6, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("mkdir -p /etc/vlf-agent", sudoPass, usePass)); err != nil {
		return logs.String(), 7, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 600 /tmp/vlf-agent.yaml /etc/vlf-agent/config.yaml", sudoPass, usePass)); err != nil {
		writeLog(logs, "install config failed")
		return logs.String(), 8, err
	}
	writeLog(logs, "config installed")

	if err := uploadBytes(ctx, client, params.ServiceContent, "/tmp/vlf-agent.service"); err != nil {
		writeLog(logs, "upload service failed")
		return logs.String(), 9, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("install -m 644 /tmp/vlf-agent.service /etc/systemd/system/vlf-agent.service", sudoPass, usePass)); err != nil {
		writeLog(logs, "install service failed")
		return logs.String(), 10, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl daemon-reload", sudoPass, usePass)); err != nil {
		return logs.String(), 11, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl enable --now vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "enable service failed")
		return logs.String(), 12, err
	}
	// Always restart after config replacement so token/config changes are applied.
	if _, _, err := runRemote(ctx, client, sudoCmd("systemctl restart vlf-agent", sudoPass, usePass)); err != nil {
		writeLog(logs, "restart service failed")
		return logs.String(), 13, err
	}
	writeLog(logs, "service started")

	if params.EnableUFW && params.AllowCIDR != "" && params.AgentPort > 0 {
		cmd := fmt.Sprintf("ufw allow from %s to any port %d proto tcp", params.AllowCIDR, params.AgentPort)
		if _, _, err := runRemote(ctx, client, sudoCmd(cmd, sudoPass, usePass)); err != nil {
			writeLog(logs, "ufw rule failed")
			return logs.String(), 14, err
		}
		writeLog(logs, "ufw rule applied")
	}

	return logs.String(), 0, nil
}

func (e *SSHExecutor) InstallVLFProto(ctx context.Context, node *db.Node, params InstallVLFProtoParams) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	client, err := e.openClient(node)
	if err != nil {
		return "", 1, err
	}
	defer client.Close()

	logs := &strings.Builder{}
	writeLog(logs, "preflight ok")

	sudoPass, usePass, err := detectSudo(ctx, client, params.SudoPasswords)
	if err != nil {
		writeLog(logs, "sudo check failed")
		return logs.String(), 2, err
	}

	installCmd := buildVLFProtoInstallCommand(params)
	// Run the whole pipeline under sudo; otherwise only `curl` runs as root.
	sudoInstallCmd := "bash -lc " + strconv.Quote(installCmd)
	out, code, err := runRemote(ctx, client, sudoCmd(sudoInstallCmd, sudoPass, usePass))
	if strings.TrimSpace(out) != "" {
		writeLog(logs, strings.TrimSpace(out))
	}
	if err != nil {
		writeLog(logs, "vlf-proto install failed")
		if code == 0 {
			code = 3
		}
		return logs.String(), code, err
	}
	writeLog(logs, "vlf-proto install finished")
	return logs.String(), 0, nil
}

func (e *SSHExecutor) InstallRemnaGeodata(ctx context.Context, node *db.Node, params RemnaGeodataParams) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	params = normalizeRemnaParams(params)

	client, err := e.openClient(node)
	if err != nil {
		return "", 1, err
	}
	defer client.Close()

	logs := &strings.Builder{}
	writeLog(logs, "preflight ok")

	sudoPass, usePass, err := detectSudo(ctx, client, params.SudoPasswords)
	if err != nil {
		writeLog(logs, "sudo check failed")
		return logs.String(), 2, err
	}

	writeLog(logs, fmt.Sprintf("release=%s repo=%s", params.ReleaseTag, params.RulesRepo))

	scriptDir := path.Dir(params.ScriptPath)
	if scriptDir == "." || strings.TrimSpace(scriptDir) == "" {
		return logs.String(), 3, errors.New("invalid script path")
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("mkdir -p %s %s", shellEscape(params.GeodataDir), shellEscape(scriptDir)),
		sudoPass,
		usePass,
	)); err != nil {
		writeLog(logs, "failed to create remnanode directories")
		return logs.String(), 4, err
	}

	composeChanged, err := e.ensureRemnaComposeMounts(ctx, client, sudoPass, usePass, params)
	if err != nil {
		writeLog(logs, "compose patch failed")
		return logs.String(), 5, err
	}
	if composeChanged {
		writeLog(logs, "compose patched: remnanode geodata mounts configured")
	} else {
		writeLog(logs, "compose already contains required geodata mounts")
	}

	scriptContent := buildRemnaGeodataScript(params)
	tmpScriptPath := "/tmp/remnanode-geodata-update.sh"
	if err := uploadBytes(ctx, client, scriptContent, tmpScriptPath); err != nil {
		writeLog(logs, "failed to upload update script")
		return logs.String(), 6, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("install -m 0755 %s %s", shellEscape(tmpScriptPath), shellEscape(params.ScriptPath)),
		sudoPass,
		usePass,
	)); err != nil {
		writeLog(logs, "failed to install update script")
		return logs.String(), 7, err
	}
	writeLog(logs, "update script installed")

	cronSpec, err := normalizeCronSpec(params.CronSchedule)
	if err != nil {
		return logs.String(), 8, err
	}
	cronContent := renderRemnaCronFile(cronSpec, params.ScriptPath, params.LogPath)
	tmpCronPath := "/tmp/remnanode-geodata.cron"
	if err := uploadBytes(ctx, client, []byte(cronContent), tmpCronPath); err != nil {
		writeLog(logs, "failed to upload cron file")
		return logs.String(), 9, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("install -m 0644 %s %s", shellEscape(tmpCronPath), shellEscape(remnaCronFilePath)),
		sudoPass,
		usePass,
	)); err != nil {
		writeLog(logs, "failed to install cron file")
		return logs.String(), 10, err
	}
	writeLog(logs, fmt.Sprintf("cron configured: %s", cronSpec))

	logrotateContent := renderRemnaLogrotate(params.LogPath)
	tmpLogrotatePath := "/tmp/remnanode-geodata.logrotate"
	if err := uploadBytes(ctx, client, []byte(logrotateContent), tmpLogrotatePath); err != nil {
		writeLog(logs, "failed to upload logrotate config")
		return logs.String(), 11, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("install -m 0644 %s %s", shellEscape(tmpLogrotatePath), shellEscape(remnaLogrotateFilePath)),
		sudoPass,
		usePass,
	)); err != nil {
		writeLog(logs, "failed to install logrotate config")
		return logs.String(), 12, err
	}
	writeLog(logs, "logrotate configured")

	logDir := path.Dir(params.LogPath)
	if logDir == "." || strings.TrimSpace(logDir) == "" {
		logDir = "/var/log"
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("mkdir -p %s && touch %s && chmod 0644 %s",
			shellEscape(logDir),
			shellEscape(params.LogPath),
			shellEscape(params.LogPath),
		),
		sudoPass,
		usePass,
	)); err != nil {
		// Non-fatal: update script also prepares log path at runtime.
		writeLog(logs, "warn: failed to prepare log file, will continue: "+strings.TrimSpace(err.Error()))
	}

	runOut, code, runErr := e.runRemnaScript(ctx, client, sudoPass, usePass, params, params.ForceReload || composeChanged)
	if strings.TrimSpace(runOut) != "" {
		writeLog(logs, strings.TrimSpace(runOut))
	}
	if runErr != nil {
		writeLog(logs, "initial geodata update failed")
		if code == 0 {
			code = 14
		}
		return logs.String(), code, runErr
	}

	writeLog(logs, "remna geodata install finished")
	return logs.String(), 0, nil
}

func (e *SSHExecutor) RunRemnaGeodata(ctx context.Context, node *db.Node, params RemnaGeodataParams) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	params = normalizeRemnaParams(params)

	client, err := e.openClient(node)
	if err != nil {
		return "", 1, err
	}
	defer client.Close()

	logs := &strings.Builder{}
	writeLog(logs, "preflight ok")

	sudoPass, usePass, err := detectSudo(ctx, client, params.SudoPasswords)
	if err != nil {
		writeLog(logs, "sudo check failed")
		return logs.String(), 2, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd("test -x "+shellEscape(params.ScriptPath), sudoPass, usePass)); err != nil {
		return logs.String(), 3, fmt.Errorf("update script not found at %s (run install first)", params.ScriptPath)
	}

	out, code, runErr := e.runRemnaScript(ctx, client, sudoPass, usePass, params, params.ForceReload)
	if strings.TrimSpace(out) != "" {
		writeLog(logs, strings.TrimSpace(out))
	}
	if runErr != nil {
		if code == 0 {
			code = 4
		}
		return logs.String(), code, runErr
	}
	writeLog(logs, "remna geodata run finished")
	return logs.String(), 0, nil
}

func (e *SSHExecutor) runRemnaScript(ctx context.Context, client *ssh.Client, sudoPass string, usePass bool, params RemnaGeodataParams, forceReload bool) (string, int, error) {
	env := remnaEnvAssignments(params, forceReload)
	cmd := "env " + strings.Join(env, " ") + " " + shellEscape(params.ScriptPath)
	return runRemote(ctx, client, sudoCmd(cmd, sudoPass, usePass))
}

func (e *SSHExecutor) ensureRemnaComposeMounts(ctx context.Context, client *ssh.Client, sudoPass string, usePass bool, params RemnaGeodataParams) (bool, error) {
	composeBytes, err := downloadBytes(client, params.ComposePath)
	if err != nil {
		return false, fmt.Errorf("read compose %s: %w", params.ComposePath, err)
	}
	patched, changed, err := patchRemnaComposeVolumes(composeBytes, params)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	tmpComposePath := "/tmp/remnanode-compose.yml"
	if err := uploadBytes(ctx, client, patched, tmpComposePath); err != nil {
		return false, err
	}
	if _, _, err := runRemote(ctx, client, sudoCmd(
		fmt.Sprintf("install -m 0644 %s %s", shellEscape(tmpComposePath), shellEscape(params.ComposePath)),
		sudoPass,
		usePass,
	)); err != nil {
		return false, err
	}
	return true, nil
}

func normalizeRemnaParams(params RemnaGeodataParams) RemnaGeodataParams {
	if strings.TrimSpace(params.RulesRepo) == "" {
		params.RulesRepo = defaultRemnaRulesRepo
	}
	if strings.TrimSpace(params.ReleaseTag) == "" {
		params.ReleaseTag = defaultRemnaReleaseTag
	}
	if strings.TrimSpace(params.GeodataDir) == "" {
		params.GeodataDir = defaultRemnaGeodataDir
	}
	if strings.TrimSpace(params.ComposePath) == "" {
		params.ComposePath = defaultRemnaComposePath
	}
	if strings.TrimSpace(params.ComposeService) == "" {
		params.ComposeService = defaultRemnaComposeService
	}
	if strings.TrimSpace(params.ScriptPath) == "" {
		params.ScriptPath = defaultRemnaScriptPath
	}
	if strings.TrimSpace(params.CronSchedule) == "" {
		params.CronSchedule = defaultRemnaCronSchedule
	}
	if strings.TrimSpace(params.LogPath) == "" {
		params.LogPath = defaultRemnaLogPath
	}
	if strings.TrimSpace(params.LockPath) == "" {
		params.LockPath = defaultRemnaLockPath
	}
	if params.MinSizeBytes <= 0 {
		params.MinSizeBytes = defaultRemnaMinSizeBytes
	}
	return params
}

func normalizeCronSpec(raw string) (string, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		spec = defaultRemnaCronSchedule
	}
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return "", fmt.Errorf("invalid cron schedule: %s", spec)
	}
	return strings.Join(parts, " "), nil
}

func renderRemnaCronFile(spec string, scriptPath string, logPath string) string {
	return fmt.Sprintf("# remnanode geodata updater\n%s root %s >> %s 2>&1\n",
		spec,
		scriptPath,
		logPath,
	)
}

func renderRemnaLogrotate(logPath string) string {
	return fmt.Sprintf(`%s {
  daily
  rotate 14
  missingok
  notifempty
  compress
  delaycompress
  copytruncate
}
`, logPath)
}

func remnaEnvAssignments(params RemnaGeodataParams, forceReload bool) []string {
	verifySHA := "1"
	if params.SkipSHA256 {
		verifySHA = "0"
	}
	force := "0"
	if forceReload {
		force = "1"
	}
	return []string{
		"RULES_REPO=" + shellEscape(params.RulesRepo),
		"RELEASE_TAG=" + shellEscape(params.ReleaseTag),
		"GEODATA_DIR=" + shellEscape(params.GeodataDir),
		"COMPOSE_PATH=" + shellEscape(params.ComposePath),
		"COMPOSE_SERVICE=" + shellEscape(params.ComposeService),
		"LOG_PATH=" + shellEscape(params.LogPath),
		"LOCK_FILE=" + shellEscape(params.LockPath),
		"MIN_BYTES=" + shellEscape(strconv.FormatInt(params.MinSizeBytes, 10)),
		"VERIFY_SHA256=" + shellEscape(verifySHA),
		"FORCE_RELOAD=" + shellEscape(force),
	}
}

func buildRemnaGeodataScript(params RemnaGeodataParams) []byte {
	verifyDefault := 1
	if params.SkipSHA256 {
		verifyDefault = 0
	}
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -Eeuo pipefail
umask 022

RULES_REPO_DEFAULT=%q
RELEASE_TAG_DEFAULT=%q
GEODATA_DIR_DEFAULT=%q
COMPOSE_PATH_DEFAULT=%q
COMPOSE_SERVICE_DEFAULT=%q
LOG_PATH_DEFAULT=%q
LOCK_FILE_DEFAULT=%q
MIN_BYTES_DEFAULT=%d
VERIFY_SHA256_DEFAULT=%d

RULES_REPO="${RULES_REPO:-$RULES_REPO_DEFAULT}"
RELEASE_TAG="${RELEASE_TAG:-$RELEASE_TAG_DEFAULT}"
GEODATA_DIR="${GEODATA_DIR:-$GEODATA_DIR_DEFAULT}"
COMPOSE_PATH="${COMPOSE_PATH:-$COMPOSE_PATH_DEFAULT}"
COMPOSE_SERVICE="${COMPOSE_SERVICE:-$COMPOSE_SERVICE_DEFAULT}"
LOG_PATH="${LOG_PATH:-$LOG_PATH_DEFAULT}"
LOCK_FILE="${LOCK_FILE:-$LOCK_FILE_DEFAULT}"
MIN_BYTES="${MIN_BYTES:-$MIN_BYTES_DEFAULT}"
VERIFY_SHA256="${VERIFY_SHA256:-$VERIFY_SHA256_DEFAULT}"
FORCE_RELOAD="${FORCE_RELOAD:-0}"

log() {
  printf "%%s %%s\n" "$(date "+%%Y-%%m-%%d %%H:%%M:%%S")" "$*"
}

fail() {
  log "ERROR: $*"
  exit 1
}

need_bin() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

download_required() {
  local url="$1"
  local out="$2"
  curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 15 "$url" -o "$out" || fail "download failed: $url"
}

download_optional() {
  local url="$1"
  local out="$2"
  curl -fsSL --retry 2 --retry-delay 2 --connect-timeout 10 "$url" -o "$out"
}

verify_size() {
  local file="$1"
  local size
  size=$(stat -c%%s "$file" 2>/dev/null || wc -c <"$file")
  [[ "$size" =~ ^[0-9]+$ ]] || fail "failed to read file size for $file"
  if [ "$size" -lt "$MIN_BYTES" ]; then
    fail "file too small ($size bytes): $file"
  fi
}

verify_sha256_if_available() {
  local data_file="$1"
  local sum_url="$2"
  local sum_file="$3"
  if [ "$VERIFY_SHA256" != "1" ]; then
    return 0
  fi
  if ! download_optional "$sum_url" "$sum_file"; then
    log "WARN: checksum file unavailable: $sum_url"
    return 0
  fi
  local expected actual
  expected=$(awk "{print \\$1}" "$sum_file" | tr -d "\r\n")
  actual=$(sha256sum "$data_file" | awk "{print \\$1}")
  [ -n "$expected" ] || fail "empty checksum in $sum_url"
  [ "$expected" = "$actual" ] || fail "sha256 mismatch for $data_file"
}

install_if_changed() {
  local src="$1"
  local dst="$2"
  local name="$3"
  if [ -f "$dst" ] && cmp -s "$src" "$dst"; then
    log "$name unchanged"
    return 1
  fi
  install -o root -g root -m 0644 "$src" "${dst}.new"
  mv -f "${dst}.new" "$dst"
  log "$name updated"
  return 0
}

need_bin curl
need_bin sha256sum
need_bin flock
need_bin docker
need_bin install
need_bin cmp

mkdir -p "$GEODATA_DIR" "$(dirname "$LOCK_FILE")" "$(dirname "$LOG_PATH")"
touch "$LOG_PATH"
chmod 0644 "$LOG_PATH"

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  fail "another update process is already running"
fi

tmp_dir=$(mktemp -d /tmp/remnanode-geodata.XXXXXX)
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

if [ "$RELEASE_TAG" = "latest" ]; then
  base_url="https://github.com/${RULES_REPO}/releases/latest/download"
else
  base_url="https://github.com/${RULES_REPO}/releases/download/${RELEASE_TAG}"
fi

log "download source: $base_url"
download_required "$base_url/geosite.dat" "$tmp_dir/geosite.dat"
download_required "$base_url/geoip.dat" "$tmp_dir/geoip.dat"

verify_size "$tmp_dir/geosite.dat"
verify_size "$tmp_dir/geoip.dat"
verify_sha256_if_available "$tmp_dir/geosite.dat" "$base_url/geosite.dat.sha256sum" "$tmp_dir/geosite.dat.sha256sum"
verify_sha256_if_available "$tmp_dir/geoip.dat" "$base_url/geoip.dat.sha256sum" "$tmp_dir/geoip.dat.sha256sum"

changed=0
if install_if_changed "$tmp_dir/geosite.dat" "$GEODATA_DIR/geosite.dat" "geosite.dat"; then
  changed=1
fi
if install_if_changed "$tmp_dir/geoip.dat" "$GEODATA_DIR/geoip.dat" "geoip.dat"; then
  changed=1
fi

if [ "$changed" -eq 1 ] || [ "$FORCE_RELOAD" = "1" ]; then
  log "applying docker compose"
  if [ -n "$COMPOSE_SERVICE" ]; then
    docker compose -f "$COMPOSE_PATH" up -d "$COMPOSE_SERVICE" || fail "docker compose up failed"
  else
    docker compose -f "$COMPOSE_PATH" up -d || fail "docker compose up failed"
  fi
  log "docker compose applied"
else
  log "no changes detected, restart skipped"
fi

log "remnanode geodata update done"
`, params.RulesRepo, params.ReleaseTag, params.GeodataDir, params.ComposePath, params.ComposeService, params.LogPath, params.LockPath, params.MinSizeBytes, verifyDefault)
	return []byte(script)
}

func patchRemnaComposeVolumes(input []byte, params RemnaGeodataParams) ([]byte, bool, error) {
	if len(bytes.TrimSpace(input)) == 0 {
		return nil, false, errors.New("compose file is empty")
	}

	var root map[string]any
	if err := yaml.Unmarshal(input, &root); err != nil {
		return nil, false, fmt.Errorf("parse compose yaml: %w", err)
	}
	servicesRaw, ok := root["services"]
	if !ok {
		return nil, false, errors.New("compose services section not found")
	}
	services, ok := toStringAnyMap(servicesRaw)
	if !ok {
		return nil, false, errors.New("compose services is not an object")
	}

	serviceName := strings.TrimSpace(params.ComposeService)
	if serviceName == "" {
		serviceName = defaultRemnaComposeService
	}
	serviceKey, serviceMap, err := findComposeService(services, serviceName)
	if err != nil {
		return nil, false, err
	}

	desiredMounts := []string{
		fmt.Sprintf("%s/geosite.dat:/usr/local/share/xray/geosite.dat:ro", strings.TrimRight(params.GeodataDir, "/")),
		fmt.Sprintf("%s/geoip.dat:/usr/local/share/xray/geoip.dat:ro", strings.TrimRight(params.GeodataDir, "/")),
	}

	updatedVolumes, changed, err := ensureComposeVolumeEntries(serviceMap["volumes"], desiredMounts)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return input, false, nil
	}

	serviceMap["volumes"] = updatedVolumes
	services[serviceKey] = serviceMap
	root["services"] = services
	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func findComposeService(services map[string]any, serviceName string) (string, map[string]any, error) {
	if raw, ok := services[serviceName]; ok {
		if serviceMap, ok := toStringAnyMap(raw); ok {
			return serviceName, serviceMap, nil
		}
	}
	for key, raw := range services {
		serviceMap, ok := toStringAnyMap(raw)
		if !ok {
			continue
		}
		containerName, _ := serviceMap["container_name"].(string)
		if strings.TrimSpace(containerName) == serviceName {
			return key, serviceMap, nil
		}
	}
	return "", nil, fmt.Errorf("remnanode service not found in compose (expected service or container_name=%s)", serviceName)
}

func ensureComposeVolumeEntries(raw any, desiredMounts []string) ([]any, bool, error) {
	entries := make([]any, 0)
	switch typed := raw.(type) {
	case nil:
		entries = []any{}
	case []any:
		entries = append(entries, typed...)
	case []string:
		for _, item := range typed {
			entries = append(entries, item)
		}
	default:
		return nil, false, errors.New("compose volumes is not a list")
	}

	changed := false
	for _, mount := range desiredMounts {
		target := composeVolumeTarget(mount)
		foundIndex := -1
		for idx, entry := range entries {
			if composeVolumeTargetFromEntry(entry) == target {
				foundIndex = idx
				break
			}
		}
		if foundIndex == -1 {
			entries = append(entries, mount)
			changed = true
			continue
		}
		existing, ok := entries[foundIndex].(string)
		if ok && strings.TrimSpace(existing) == strings.TrimSpace(mount) {
			continue
		}
		entries[foundIndex] = mount
		changed = true
	}
	return entries, changed, nil
}

func composeVolumeTargetFromEntry(entry any) string {
	if raw, ok := entry.(string); ok {
		return composeVolumeTarget(raw)
	}
	if obj, ok := toStringAnyMap(entry); ok {
		if val, ok := obj["target"].(string); ok {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func composeVolumeTarget(mount string) string {
	parts := strings.Split(strings.TrimSpace(mount), ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func toStringAnyMap(raw any) (map[string]any, bool) {
	switch typed := raw.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		conv := make(map[string]any, len(typed))
		for key, val := range typed {
			strKey, ok := key.(string)
			if !ok {
				return nil, false
			}
			conv[strKey] = val
		}
		return conv, true
	default:
		return nil, false
	}
}

func buildVLFProtoInstallCommand(params InstallVLFProtoParams) string {
	args := []string{
		"--repo", shellEscape(strings.TrimSpace(params.RepoURL)),
		"--ref", shellEscape(strings.TrimSpace(params.Ref)),
		"--go-version", shellEscape(strings.TrimSpace(params.GoVersion)),
		"--install-dir", shellEscape(strings.TrimSpace(params.InstallDir)),
		"--port-tcp", strconv.Itoa(params.PortTCP),
		"--port-udp", strconv.Itoa(params.PortUDP),
		"--port-udp-alt", strconv.Itoa(params.PortUDPAlt),
		"--metrics-addr", shellEscape(strings.TrimSpace(params.MetricsAddr)),
		"--metrics-port", strconv.Itoa(params.MetricsPort),
		"--client-id", shellEscape(strings.TrimSpace(params.ClientID)),
		"--log-level", shellEscape(strings.TrimSpace(params.LogLevel)),
	}
	if !params.EnableMetrics {
		args = append(args, "--no-metrics")
	}
	if params.EnableUFW {
		args = append(args, "--ufw")
	}
	if strings.TrimSpace(params.Domain) != "" {
		args = append(args, "--domain", shellEscape(strings.TrimSpace(params.Domain)))
	}
	if strings.TrimSpace(params.TLSServerName) != "" {
		args = append(args, "--tls-server-name", shellEscape(strings.TrimSpace(params.TLSServerName)))
	}
	if strings.TrimSpace(params.Secret) != "" {
		args = append(args, "--secret", shellEscape(strings.TrimSpace(params.Secret)))
	}
	if params.ShowSecrets {
		args = append(args, "--show-secrets")
	}
	if params.Force {
		args = append(args, "--force")
	}
	return "curl -fsSL https://raw.githubusercontent.com/skalover32-a11y/VLF-Proto/main/scripts/install.sh | bash -s -- " + strings.Join(args, " ")
}

func (e *SSHExecutor) CheckAgentInstalled(ctx context.Context, node *db.Node, agentPort int) (bool, string, error) {
	client, err := e.openClient(node)
	if err != nil {
		return false, "", err
	}
	defer client.Close()
	port := agentPort
	if port <= 0 {
		port = 9191
	}
	healthCurl := fmt.Sprintf("curl -fsS --max-time 2 http://127.0.0.1:%d/health >/dev/null 2>&1", port)
	if e != nil && e.Encryptor != nil && node != nil && node.AgentTokenEnc != nil && strings.TrimSpace(*node.AgentTokenEnc) != "" {
		if token, decErr := e.Encryptor.DecryptString(*node.AgentTokenEnc); decErr == nil && strings.TrimSpace(token) != "" {
			header := "Authorization: Bearer " + strings.TrimSpace(token)
			healthCurl = fmt.Sprintf("curl -fsS --max-time 2 -H %s http://127.0.0.1:%d/health >/dev/null 2>&1", shellEscape(header), port)
		}
	}
	cmd := fmt.Sprintf(
		"if [ -x /usr/local/bin/vlf-agent ]; then echo binary=1; else echo binary=0; fi; "+
			"if systemctl is-active --quiet vlf-agent; then echo active=1; else echo active=0; fi; "+
			"if command -v curl >/dev/null 2>&1 && %s; then echo health=1; else echo health=0; fi",
		healthCurl,
	)
	out, _, runErr := runRemote(ctx, client, cmd)
	if runErr != nil {
		return false, strings.TrimSpace(out), runErr
	}
	compact := strings.TrimSpace(strings.ReplaceAll(out, "\n", " "))
	binaryOK := strings.Contains(out, "binary=1")
	activeOK := strings.Contains(out, "active=1")
	healthOK := strings.Contains(out, "health=1")
	installed := binaryOK && activeOK && healthOK
	if compact == "" {
		compact = fmt.Sprintf("binary=%t active=%t health=%t", binaryOK, activeOK, healthOK)
	}
	return installed, compact, nil
}

func (e *SSHExecutor) RestartService(ctx context.Context, node *db.Node, service string) (string, int, error) {
	return "", 1, errors.New("restart_service not supported via ssh")
}

func (e *SSHExecutor) openClient(node *db.Node) (*ssh.Client, error) {
	if node == nil {
		return nil, errors.New("node missing")
	}
	if !node.SSHEnabled {
		return nil, errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return nil, errors.New("unsupported ssh auth method")
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return nil, errors.New("ssh config missing")
	}
	if e == nil || e.Encryptor == nil {
		return nil, errors.New("encryptor missing")
	}
	key, err := e.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            node.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func runRemote(ctx context.Context, client *ssh.Client, cmd string) (string, int, error) {
	if client == nil {
		return "", 1, errors.New("ssh client missing")
	}
	session, err := client.NewSession()
	if err != nil {
		return "", 1, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Start("bash -lc " + strconv.Quote(cmd)); err != nil {
		return "", 1, err
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return stdoutBuf.String() + stderrBuf.String(), 1, ctx.Err()
	case err := <-done:
		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			}
			return stdoutBuf.String() + stderrBuf.String(), exitCode, err
		}
		return stdoutBuf.String() + stderrBuf.String(), 0, nil
	}
}

func detectSudo(ctx context.Context, client *ssh.Client, passwords []string) (string, bool, error) {
	if _, code, err := runRemote(ctx, client, "sudo -n true"); err == nil && code == 0 {
		return "", false, nil
	}
	for _, pass := range passwords {
		trim := strings.TrimSpace(pass)
		if trim == "" {
			continue
		}
		cmd := fmt.Sprintf("echo %s | sudo -S -p '' true", shellEscape(trim))
		if _, code, err := runRemote(ctx, client, cmd); err == nil && code == 0 {
			return trim, true, nil
		}
	}
	return "", false, errors.New("sudo password required")
}

func sudoCmd(cmd string, pass string, usePass bool) string {
	if usePass && pass != "" {
		return fmt.Sprintf("echo %s | sudo -S -p '' %s", shellEscape(pass), cmd)
	}
	return "sudo -n " + cmd
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\'"\'"'`) + "'"
}

func uploadFile(ctx context.Context, client *ssh.Client, localPath string, remotePath string) error {
	if client == nil {
		return errors.New("ssh client missing")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return uploadReader(ctx, client, file, remotePath)
}

func uploadBytes(ctx context.Context, client *ssh.Client, data []byte, remotePath string) error {
	if len(data) == 0 {
		return errors.New("empty payload")
	}
	return uploadReader(ctx, client, bytes.NewReader(data), remotePath)
}

func uploadReader(ctx context.Context, client *ssh.Client, reader io.Reader, remotePath string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer sftpClient.Close()
	remote, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer remote.Close()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(remote, reader)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func downloadBytes(client *ssh.Client, remotePath string) ([]byte, error) {
	if client == nil {
		return nil, errors.New("ssh client missing")
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer sftpClient.Close()
	remote, err := sftpClient.Open(remotePath)
	if err != nil {
		return nil, err
	}
	defer remote.Close()
	return io.ReadAll(remote)
}

func writeLog(buf *strings.Builder, line string) {
	if buf == nil {
		return
	}
	if buf.Len() > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString(line)
}

func isAptLockError(output string) bool {
	out := strings.ToLower(output)
	return strings.Contains(out, "could not get lock") ||
		strings.Contains(out, "unable to acquire the dpkg frontend lock") ||
		strings.Contains(out, "could not open lock file") ||
		strings.Contains(out, "another process") && strings.Contains(out, "lock")
}

func (e *SSHExecutor) runUpdatePrecheck(ctx context.Context, node *db.Node, params UpdateParams) (string, int, error) {
	var lines []string
	exitCode := 0

	servicemgrOut, _, servicemgrErr := e.runCommand(ctx, node, "command -v service-manager", false)
	if servicemgrErr != nil {
		if isExitError(servicemgrErr) {
			lines = append(lines, "ERR: service-manager missing")
			exitCode = 10
		} else {
			return servicemgrOut, 10, servicemgrErr
		}
	} else {
		lines = append(lines, "OK: service-manager present")
	}

	expectOut, _, expectErr := e.runCommand(ctx, node, "command -v expect", false)
	if expectErr != nil {
		if isExitError(expectErr) {
			lines = append(lines, "ERR: expect missing")
		} else {
			return expectOut, 11, expectErr
		}
	} else {
		lines = append(lines, "OK: expect present")
	}
	if params.InstallExpect {
		lines = append(lines, "INFO: install_expect requested, skipped in precheck_only")
	}

	versionOut, _, _ := e.runCommand(ctx, node, "bash -lc \"service-manager version || true\"", false)
	if strings.TrimSpace(versionOut) != "" {
		lines = append(lines, "service-manager version: "+strings.TrimSpace(versionOut))
	}

	sudoOut, _, sudoErr := e.runCommand(ctx, node, "sudo -n true", false)
	if sudoErr != nil {
		if isExitError(sudoErr) {
			lines = append(lines, "ERR: sudo -n failed (passwordless sudo missing)")
		} else {
			return sudoOut, 12, sudoErr
		}
	} else {
		lines = append(lines, "OK: sudo -n available")
	}

	logText := strings.Join(lines, "\n")
	if exitCode != 0 {
		return logText, exitCode, fmt.Errorf("precheck failed")
	}
	return logText, 0, nil
}

func isExitError(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr)
}

func (e *SSHExecutor) runCommand(ctx context.Context, node *db.Node, cmd string, allowDisconnect bool) (string, int, error) {
	if node == nil {
		return "", 1, errors.New("node missing")
	}
	if !node.SSHEnabled {
		return "", 1, errors.New("ssh disabled")
	}
	if strings.TrimSpace(node.SSHAuthMethod) != "" && strings.ToLower(node.SSHAuthMethod) != "key" {
		return "", 1, errors.New("unsupported ssh auth method")
	}
	if strings.TrimSpace(node.SSHHost) == "" || node.SSHPort == 0 || strings.TrimSpace(node.SSHUser) == "" {
		return "", 1, errors.New("ssh config missing")
	}
	if e == nil || e.Encryptor == nil {
		return "", 1, errors.New("encryptor missing")
	}
	key, err := e.Encryptor.DecryptString(node.SSHKeyEnc)
	if err != nil {
		return "", 1, err
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return "", 1, err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            node.SSHUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(node.SSHHost, fmt.Sprintf("%d", node.SSHPort))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", 1, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return "", 1, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", 1, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err := session.Start(cmd); err != nil {
		return "", 1, err
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return stdoutBuf.String() + stderrBuf.String(), 1, ctx.Err()
	case err := <-waitCh:
		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*ssh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			}
			if allowDisconnect && isDisconnectError(err) {
				return stdoutBuf.String() + stderrBuf.String(), 0, nil
			}
			return stdoutBuf.String() + stderrBuf.String(), exitCode, err
		}
		return stdoutBuf.String() + stderrBuf.String(), 0, nil
	}
}

func isDisconnectError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset") || strings.Contains(msg, "closed network connection") || strings.Contains(msg, "eof") {
		return true
	}
	return false
}

func buildservicemgrUpdateCommand() string {
	script := `flock -n /var/lock/service-manager-update.lock -c "expect <<'EOF'
set timeout 60
set env(TERM) \"dumb\"
log_user 1
match_max 200000
spawn service-manager
expect {
  -re {Please enter your selection.*} { send \"2\r\" }
  -re {Enter.*selection.*} { send \"2\r\" }
  -re {Enter.*choice.*} { send \"2\r\" }
  timeout { puts \"ERROR: timeout waiting for menu\"; exit 2 }
}
set timeout 900
expect {
  -re {Already.*latest} { puts \"INFO: already latest version\"; exit 0 }
  -re {Update.*(completed|success|finished)} { puts \"INFO: update completed\"; exit 0 }
  -re {Please enter your selection.*} { puts \"INFO: update finished, returned to menu\"; exit 0 }
  eof { puts \"INFO: service-manager exited after update\"; exit 0 }
  timeout { puts \"ERROR: update timeout\"; exit 3 }
}
EOF"
rc=$?
if [ $rc -eq 1 ]; then
  exit 20
fi
exit $rc
`
	return "bash -lc " + strconv.Quote(script)
}
