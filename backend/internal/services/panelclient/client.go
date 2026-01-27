package panelclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	baseURL   string
	username  string
	password  string
	verifyTLS bool
	client    *http.Client
}

func New(baseURL, username, password string, verifyTLS bool) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		username:  username,
		password:  password,
		verifyTLS: verifyTLS,
		client: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   15 * time.Second,
		},
	}, nil
}

func (c *Client) Login() error {
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)
	endpoint, err := c.joinURL("/login")
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("login failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) ListInbounds() (map[string]any, error) {
	endpoint, err := c.joinURL("/panel/api/inbounds/list")
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodGet, endpoint, nil)
}

func (c *Client) OnlineClients() (map[string]any, error) {
	endpoints := []string{
		"/panel/api/inbounds/onlines",
		"/panel/api/inbounds/online",
	}
	var lastErr error
	for _, suffix := range endpoints {
		endpoint, err := c.joinURL(suffix)
		if err != nil {
			lastErr = err
			continue
		}
		methods := []string{http.MethodGet, http.MethodPost}
		for _, method := range methods {
			resp, err := c.doJSON(method, endpoint, nil)
			if err != nil {
				if isNotFoundErr(err) {
					lastErr = err
					continue
				}
				return nil, err
			}
			return resp, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("online endpoint not available")
	}
	return nil, lastErr
}

func (c *Client) AddInbound(payload map[string]any) (map[string]any, error) {
	endpoint, err := c.joinURL("/panel/api/inbounds/add")
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodPost, endpoint, normalizePayload(payload))
}

func (c *Client) UpdateInbound(id string, payload map[string]any) (map[string]any, error) {
	endpoint, err := c.joinURL("/panel/api/inbounds/update/" + id)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodPost, endpoint, normalizePayload(payload))
}

func (c *Client) DeleteInbound(id string) (map[string]any, error) {
	endpoint, err := c.joinURL("/panel/api/inbounds/del/" + id)
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodPost, endpoint, map[string]any{})
}

func (c *Client) RestartXray() (map[string]any, error) {
	endpoint, err := c.joinURL("/panel/api/server/restartXrayService")
	if err != nil {
		return nil, err
	}
	return c.doJSON(http.MethodPost, endpoint, map[string]any{})
}

func (c *Client) doJSON(method, url string, body map[string]any) (map[string]any, error) {
	var buf *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(raw)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
	}
	var data map[string]any
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "status 404") ||
		strings.Contains(msg, "status 405") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "method not allowed")
}

func (c *Client) joinURL(suffix string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	suffix = strings.TrimPrefix(suffix, "/")
	if basePath == "" {
		u.Path = "/" + suffix
	} else {
		u.Path = path.Join(basePath, suffix)
	}
	return u.String(), nil
}

func normalizePayload(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "settings" || k == "streamSettings" {
			switch v.(type) {
			case map[string]any, []any:
				raw, _ := json.Marshal(v)
				out[k] = string(raw)
				continue
			}
		}
		out[k] = v
	}
	return out
}
