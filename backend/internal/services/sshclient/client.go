package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type Client struct {
	Timeout time.Duration
}

type Error struct {
	Stage string
	Err   error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(timeout time.Duration) *Client {
	return &Client{Timeout: timeout}
}

func (c *Client) RunWithOutput(ctx context.Context, host string, port int, user string, privateKeyPEM string, cmd string) (string, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return "", &Error{Stage: "auth", Err: fmt.Errorf("invalid key format: %w", err)}
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         c.Timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: c.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", &Error{Stage: "dial", Err: err}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return "", &Error{Stage: classifyHandshakeStage(err), Err: err}
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", &Error{Stage: "command", Err: err}
	}
	defer session.Close()

	out, err := session.StdoutPipe()
	if err != nil {
		return "", &Error{Stage: "command", Err: err}
	}
	errPipe, err := session.StderrPipe()
	if err != nil {
		return "", &Error{Stage: "command", Err: err}
	}

	if err := session.Start(cmd); err != nil {
		return "", &Error{Stage: "command", Err: err}
	}

	done := make(chan error, 1)
	var stdoutBuf, stderrBuf strings.Builder
	go func() {
		_, _ = io.Copy(&stdoutBuf, out)
		_, _ = io.Copy(&stderrBuf, errPipe)
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		return "", &Error{Stage: "command", Err: ctx.Err()}
	case err := <-done:
		if err != nil {
			msg := strings.TrimSpace(stderrBuf.String())
			if msg == "" {
				msg = err.Error()
			}
			return stdoutBuf.String(), &Error{Stage: "command", Err: fmt.Errorf(msg)}
		}
		return stdoutBuf.String(), nil
	}
}

func (c *Client) Run(ctx context.Context, host string, port int, user string, privateKeyPEM string, cmd string) error {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return &Error{Stage: "auth", Err: fmt.Errorf("invalid key format: %w", err)}
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         c.Timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: c.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return &Error{Stage: "dial", Err: err}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return &Error{Stage: classifyHandshakeStage(err), Err: err}
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return &Error{Stage: "command", Err: err}
	}
	defer session.Close()

	if err := session.Start(cmd); err != nil {
		return &Error{Stage: "command", Err: err}
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()
	select {
	case <-ctx.Done():
		return &Error{Stage: "command", Err: ctx.Err()}
	case err := <-done:
		if err != nil {
			return &Error{Stage: "command", Err: err}
		}
		return nil
	}
}

func (c *Client) Reboot(ctx context.Context, host string, port int, user string, privateKeyPEM string) error {
	return c.Run(ctx, host, port, user, privateKeyPEM, "sudo /sbin/reboot")
}

func classifyHandshakeStage(err error) string {
	var authErr *ssh.ServerAuthError
	if errors.As(err, &authErr) {
		return "auth"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain") {
		return "auth"
	}
	return "handshake"
}
