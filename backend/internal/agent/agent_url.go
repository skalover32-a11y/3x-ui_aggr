package agent

import (
	"bufio"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"strings"
)

func ResolveURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	host := parsed.Hostname()
	resolved := ResolveHost(host)
	if resolved == "" || strings.EqualFold(resolved, host) {
		return raw
	}
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(resolved, port)
	} else {
		parsed.Host = resolved
	}
	return parsed.String()
}

func ResolveHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return host
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || host == "127.0.0.1" || host == "::1" {
		if gw := DockerGateway(); gw != "" {
			return gw
		}
		return host
	}
	for _, publicHost := range publicHosts() {
		if publicHost != "" && strings.EqualFold(publicHost, host) {
			if gw := DockerGateway(); gw != "" {
				return gw
			}
			return host
		}
	}
	return host
}

func DockerGateway() string {
	if raw := strings.TrimSpace(os.Getenv("AGG_DOCKER_HOST_GATEWAY")); raw != "" {
		return raw
	}
	if gw := detectDefaultGateway(); gw != "" {
		return gw
	}
	return "172.17.0.1"
}

func DockerSubnet() string {
	gw := DockerGateway()
	ip := net.ParseIP(gw)
	if ip == nil {
		return ""
	}
	ip = ip.To4()
	if ip == nil {
		return ""
	}
	return net.IPv4(ip[0], ip[1], 0, 0).String() + "/16"
}

func publicHosts() []string {
	out := []string{}
	if raw := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			if host := strings.TrimSpace(parsed.Hostname()); host != "" {
				out = append(out, host)
			}
		}
	}
	if raw := strings.TrimSpace(os.Getenv("AGG_ALLOW_CIDR")); raw != "" {
		host := strings.TrimSpace(strings.Split(raw, "/")[0])
		if host != "" {
			out = append(out, host)
		}
	}
	return out
}

func detectDefaultGateway() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		gwHex := fields[2]
		if gwHex == "00000000" {
			continue
		}
		ip := parseHexGateway(gwHex)
		if ip != "" {
			return ip
		}
	}
	return ""
}

func parseHexGateway(hexStr string) string {
	data, err := hex.DecodeString(hexStr)
	if err != nil || len(data) != 4 {
		return ""
	}
	return net.IPv4(data[3], data[2], data[1], data[0]).String()
}
