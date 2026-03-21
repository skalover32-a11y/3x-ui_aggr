package checks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lib/pq"

	"agr_3x_ui/internal/db"
	"agr_3x_ui/internal/security"
)

func TestExecuteServiceCheckHTTPExpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/404" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	worker := &Worker{}
	node := &db.Node{VerifyTLS: true}
	check := &db.Check{Type: "HTTP", TimeoutMS: 1000, Retries: 0}
	service := &db.Service{
		Kind:           "CUSTOM_HTTP",
		URL:            stringPtr(srv.URL),
		HealthPath:     stringPtr("/"),
		ExpectedStatus: pq.Int64Array{200},
		IsEnabled:      true,
	}

	ok, _, statusCode, _, _ := worker.executeServiceCheck(context.Background(), node, service, check)
	if !ok || statusCode != http.StatusOK {
		t.Fatalf("expected ok status 200, got ok=%v status=%d", ok, statusCode)
	}

	service.HealthPath = stringPtr("/404")
	ok, _, statusCode, _, _ = worker.executeServiceCheck(context.Background(), node, service, check)
	if ok || statusCode != http.StatusNotFound {
		t.Fatalf("expected fail status 404, got ok=%v status=%d", ok, statusCode)
	}
}

func TestExecuteServiceCheckFTPExpectedReplyCode(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = fmt.Fprint(c, "220 FTP ready\r\n")
			}(conn)
		}
	}()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected TCP addr, got %T", listener.Addr())
	}

	worker := &Worker{}
	node := &db.Node{}
	check := &db.Check{Type: "FTP", TimeoutMS: 1000, Retries: 0}
	service := &db.Service{
		Kind:           "CUSTOM_FTP",
		Host:           stringPtr("127.0.0.1"),
		Port:           intPtr(tcpAddr.Port),
		ExpectedStatus: pq.Int64Array{220},
		IsEnabled:      true,
	}

	ok, _, statusCode, _, errMsg := worker.executeServiceCheck(context.Background(), node, service, check)
	if !ok || statusCode != 220 || errMsg != nil {
		t.Fatalf("expected ok ftp status 220, got ok=%v status=%d err=%v", ok, statusCode, errMsg)
	}
}

func TestExecuteServiceCheckFTPWithAuthExpectedReplyCode(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				_, _ = fmt.Fprint(c, "220 FTP ready\r\n")
				line, _ := reader.ReadString('\n')
				if strings.TrimSpace(line) != "USER backup" {
					_, _ = fmt.Fprint(c, "530 invalid user\r\n")
					return
				}
				_, _ = fmt.Fprint(c, "331 password required\r\n")
				line, _ = reader.ReadString('\n')
				if strings.TrimSpace(line) != "PASS s3cret" {
					_, _ = fmt.Fprint(c, "530 invalid password\r\n")
					return
				}
				_, _ = fmt.Fprint(c, "230 login ok\r\n")
			}(conn)
		}
	}()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected TCP addr, got %T", listener.Addr())
	}

	enc, err := security.NewEncryptor(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	passwordEnc, err := enc.EncryptString("s3cret")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	worker := &Worker{Encryptor: enc}
	node := &db.Node{}
	check := &db.Check{Type: "FTP", TimeoutMS: 1000, Retries: 0}
	service := &db.Service{
		Kind:            "CUSTOM_FTP",
		Host:            stringPtr("127.0.0.1"),
		Port:            intPtr(tcpAddr.Port),
		AuthUsername:    stringPtr("backup"),
		AuthPasswordEnc: &passwordEnc,
		ExpectedStatus:  pq.Int64Array{230},
		IsEnabled:       true,
	}

	ok, _, statusCode, _, errMsg := worker.executeServiceCheck(context.Background(), node, service, check)
	if !ok || statusCode != 230 || errMsg != nil {
		t.Fatalf("expected ok ftp status 230, got ok=%v status=%d err=%v", ok, statusCode, errMsg)
	}
}

func stringPtr(value string) *string {
	v := value
	return &v
}

func intPtr(value int) *int {
	v := value
	return &v
}
