package transport

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type Endpoint struct {
	Host      string
	Port      int
	User      string
	Identity  string
	EIPS      string // remote eips path; defaults to /usr/sbin/eips
	RemotePNG string // remote PNG buffer path; defaults to /tmp/dash.png
}

func (e Endpoint) eipsPath() string {
	if e.EIPS != "" {
		return e.EIPS
	}
	return "/usr/sbin/eips"
}

func (e Endpoint) remotePNGPath() string {
	if e.RemotePNG != "" {
		return e.RemotePNG
	}
	return "/tmp/dash.png"
}

type Client struct {
	ep Endpoint

	mu     sync.Mutex
	client *ssh.Client
}

// Dial establishes the initial SSH connection.
func Dial(ctx context.Context, ep Endpoint) (*Client, error) {
	sc, err := dialSSH(ctx, ep)
	if err != nil {
		return nil, err
	}
	return &Client{ep: ep, client: sc}, nil
}

// dialSSH builds the credential set + host-key callback for ep and opens a fresh ssh.Client.
func dialSSH(ctx context.Context, ep Endpoint) (*ssh.Client, error) {
	auth, err := buildAuth(ep.Identity)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no SSH credentials: no usable key found in ~/.ssh and ssh-agent unavailable")
	}
	hk, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}

	addr := net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))

	cfg := &ssh.ClientConfig{
		User:            ep.User,
		Auth:            auth,
		HostKeyCallback: hk,
		Timeout:         5 * time.Second,
	}
	// Constrain host key algorithm to what we have on file for this host —
	// otherwise the server may offer an algo we trust (ED25519) AND others we don't,
	// and Go's default selection can pick a key type we don't have a TOFU entry for.
	if algos := KnownHostKeyAlgorithms(ep.Host); len(algos) > 0 {
		cfg.HostKeyAlgorithms = algos
	}

	d := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func (c *Client) Probe(ctx context.Context) error {
	sess, err := c.newSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()
	if _, err := sess.CombinedOutput("echo kindle-dash-probe-ok"); err != nil {
		return fmt.Errorf("remote exec: %w", err)
	}
	return nil
}

func (c *Client) RunCommand(cmd string) (string, error) {
	sess, err := c.newSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

// PushRegion writes pngBytes to the Kindle's PNG buffer and tells eips to
// repaint just (x, y) using the supplied waveform (e.g. "du" for fast).
func (c *Client) PushRegion(pngBytes []byte, x, y int, waveform string) error {
	cmd := fmt.Sprintf("cat > %s && %s -g %s -x %d -y %d -w %s",
		c.ep.remotePNGPath(), c.ep.eipsPath(), c.ep.remotePNGPath(), x, y, waveform)
	return c.runWithStdin(cmd, pngBytes)
}

// FullRefresh writes pngBytes and forces a full-screen refresh with -f. If
// `clear`, the Kindle wipes to white first with `eips -c` (cleaner but slow).
func (c *Client) FullRefresh(pngBytes []byte, waveform string, clear bool) error {
	cmd := fmt.Sprintf("cat > %s", c.ep.remotePNGPath())
	if clear {
		cmd += fmt.Sprintf(" && %s -c", c.ep.eipsPath())
	}
	cmd += fmt.Sprintf(" && %s -g %s -w %s -f", c.ep.eipsPath(), c.ep.remotePNGPath(), waveform)
	return c.runWithStdin(cmd, pngBytes)
}

// runWithStdin pipes stdin into a fresh session and runs cmd. One reconnect
// attempt on session-open failure (handles transient ssh.Client EOFs cleanly).
func (c *Client) runWithStdin(cmd string, stdin []byte) error {
	sess, err := c.newSession()
	if err != nil {
		if rerr := c.reconnect(); rerr != nil {
			return fmt.Errorf("session %v; reconnect failed: %v", err, rerr)
		}
		sess, err = c.newSession()
		if err != nil {
			return fmt.Errorf("session after reconnect: %w", err)
		}
	}
	defer sess.Close()
	sess.Stdin = bytes.NewReader(stdin)
	if err := sess.Run(cmd); err != nil {
		return fmt.Errorf("remote %q: %w", cmd, err)
	}
	return nil
}

func (c *Client) newSession() (*ssh.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, fmt.Errorf("ssh client closed")
	}
	return c.client.NewSession()
}

func (c *Client) reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fresh, err := dialSSH(ctx, c.ep)
	if err != nil {
		return err
	}
	c.client = fresh
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}
