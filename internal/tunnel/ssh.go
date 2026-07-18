package tunnel

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const sshServerAddr = "31.220.74.109:22"
const sshUser = "root"

var sshPrivateKey = []byte(`-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBHfkWnyHstJ288x9b5my7SwSnBjpfs/JW4cEqW2GhFVQAAAJircCXXq3Al
1wAAAAtzc2gtZWQyNTUxOQAAACBHfkWnyHstJ288x9b5my7SwSnBjpfs/JW4cEqW2GhFVQ
AAAEC3TO8922UNhTtyPDCNG8UUwafcB5D4Rs6ZYorAoFqEJUd+RafIey0nbzzH1vmbLtLB
KcGOl+z8lbhwSpbYaEVVAAAAFGRhbGppbmFjMi1hZ2VudC0yMDI2AQ==
-----END OPENSSH PRIVATE KEY-----`)

type SSHTunnel struct {
	localPort     int
	serviceName   string
	url           string
	stopCh        chan struct{}
	onConnected   func(url string)
	running       bool
	mu            sync.Mutex
	remotePort    int
	client        *ssh.Client
	lastConnected time.Time
}

func (t *SSHTunnel) LastConnected() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastConnected
}

func NewSSH(localPort int, hostname string, onConnected func(url string)) *SSHTunnel {
	sanitized := sanitizeHostname(hostname)
	return &SSHTunnel{
		localPort:   localPort,
		serviceName: sanitized,
		stopCh:      make(chan struct{}),
		onConnected: onConnected,
		remotePort:  getSSHPort(sanitized),
	}
}

func sanitizeHostname(name string) string {
	sanitized := strings.ToLower(name)
	sanitized = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(sanitized)
	var clean strings.Builder
	for _, r := range sanitized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			clean.WriteRune(r)
		}
	}
	sanitized = strings.Trim(clean.String(), "-")
	if len(sanitized) < 2 {
		sanitized = "machine"
	}
	return sanitized
}

func getSSHPort(name string) int {
	m := map[string]int{
		"desktop-inj3o0l":   7181,
		"desktop-s43ukd6":   7182,
		"usermic-m3sii9l":   7183,
		"desktop-ba967g1":   7184,
		"sandokan":          7185,
	}
	if p, ok := m[name]; ok {
		return p
	}
	return 7181
}

func (t *SSHTunnel) writeKey() (string, error) {
	keyDir := filepath.Join(os.Getenv("ProgramData"), "daljinac2", ".ssh")
	os.MkdirAll(keyDir, 0700)
	keyPath := filepath.Join(keyDir, "id_daljinac2")
	if _, err := os.Stat(keyPath); err == nil {
		return keyPath, nil
	}
	if err := os.WriteFile(keyPath, sshPrivateKey, 0600); err != nil {
		return "", fmt.Errorf("write key: %w", err)
	}
	return keyPath, nil
}

func (t *SSHTunnel) Start() {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()
	go t.Run()
}

func (t *SSHTunnel) Run() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, true)
			log.Printf("[ssh] PANIC: %v\n%s", r, buf[:n])
		}
	}()

	t.running = true
	delay := 2 * time.Second

	for {
		select {
		case <-t.stopCh:
			t.mu.Lock()
			t.running = false
			t.mu.Unlock()
			return
		default:
		}

		t.connect()

		t.mu.Lock()
		isRunning := t.running
		t.mu.Unlock()
		if !isRunning {
			return
		}

		select {
		case <-t.stopCh:
			t.mu.Lock()
			t.running = false
			t.mu.Unlock()
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (t *SSHTunnel) connect() {
	signer, err := ssh.ParsePrivateKey(sshPrivateKey)
	if err != nil {
		log.Printf("[ssh] parse key error: %v", err)
		return
	}

	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", sshServerAddr, config)
	if err != nil {
		log.Printf("[ssh] dial error: %v", err)
		return
	}

	t.mu.Lock()
	t.client = client
	t.lastConnected = time.Now()
	t.url = fmt.Sprintf("http://31.220.74.109:%d", t.remotePort)
	cb := t.onConnected
	t.mu.Unlock()

	if cb != nil {
		cb(t.url)
	}

	var listener net.Listener
	port := t.remotePort
	for ; port <= 7200; port++ {
		type lr struct {
			l net.Listener
			e error
		}
		ch := make(chan lr, 1)
		go func(p int) {
			l, e := client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", p))
			ch <- lr{l, e}
		}(port)
		select {
		case res := <-ch:
			err = res.e
			listener = res.l
		case <-time.After(2 * time.Second):
			log.Printf("[ssh] port %d: listen timed out", port)
			client.Close()
			return
		}
		if err == nil {
			t.mu.Lock()
			t.remotePort = port
			t.url = fmt.Sprintf("http://31.220.74.109:%d", port)
			t.mu.Unlock()
			break
		}
	}
	if listener == nil {
		log.Printf("[ssh] no free port in range %d-7200", t.remotePort)
		client.Close()
		return
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-t.stopCh:
				return
			case <-ticker.C:
				t.mu.Lock()
				t.lastConnected = time.Now()
				t.mu.Unlock()
			}
		}
	}()

	acceptCh := make(chan net.Conn)
	go func() {
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				select {
				case <-t.stopCh:
				default:
				}
				close(acceptCh)
				return
			}
			acceptCh <- remoteConn
		}
	}()

	for {
		select {
		case <-t.stopCh:
			listener.Close()
			client.Close()
			return
		case remoteConn, ok := <-acceptCh:
			if !ok {
				listener.Close()
				client.Close()
				return
			}
			go t.handleConnection(remoteConn)
		}
	}
}

func (t *SSHTunnel) handleConnection(remoteConn net.Conn) {
	defer remoteConn.Close()

	localConn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", t.localPort), 10*time.Second)
	if err != nil {
		return
	}
	defer localConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		cp(remoteConn, localConn)
		wg.Done()
	}()
	go func() {
		cp(localConn, remoteConn)
		wg.Done()
	}()
	wg.Wait()
}

func cp(dst net.Conn, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
}

func (t *SSHTunnel) Stop() {
	t.mu.Lock()
	t.running = false
	if t.client != nil {
		t.client.Close()
	}
	t.mu.Unlock()
	close(t.stopCh)
}

func (t *SSHTunnel) URL() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.url
}

func (t *SSHTunnel) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}
