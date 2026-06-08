package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func KnownHostsPath() string {
	return filepath.Join(userSSHDir(), "known_hosts")
}

// hostKeyCallback returns a callback that:
//   - on first sight of a host: appends to known_hosts and accepts (TOFU)
//   - on key mismatch: refuses with an actionable error
func hostKeyCallback() (ssh.HostKeyCallback, error) {
	khPath := KnownHostsPath()
	if _, err := os.Stat(khPath); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(khPath), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(khPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		f.Close()
	}

	base, err := knownhosts.New(khPath)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		var kerr *knownhosts.KeyError
		if errors.As(err, &kerr) {
			if len(kerr.Want) == 0 {
				return appendKnownHost(khPath, hostname, remote, key)
			}
			return fmt.Errorf("host key mismatch for %s — refusing to connect. If you reinstalled the Kindle, remove the matching line from %s and retry", hostname, khPath)
		}
		return err
	}, nil
}

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname), knownhosts.Normalize(remote.String())}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// KnownHostKeyAlgorithms returns the ssh key-type strings (e.g. "ssh-ed25519")
// that known_hosts has on file for `host`. Used to constrain ClientConfig
// HostKeyAlgorithms so the server can't trick us into negotiating a type we
// haven't TOFU'd. Empty result = host unknown; caller should leave the field
// unset to allow first-contact.
func KnownHostKeyAlgorithms(host string) []string {
	data, err := os.ReadFile(KnownHostsPath())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		match := false
		for _, h := range strings.Split(fields[0], ",") {
			h = strings.TrimPrefix(h, "[")
			if i := strings.LastIndex(h, "]:"); i >= 0 {
				h = h[:i]
			}
			if h == host {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if !seen[fields[1]] {
			seen[fields[1]] = true
			out = append(out, fields[1])
		}
	}
	return out
}

// HasKnownHost is a coarse check used by `doctor`: scan known_hosts text-wise
// for any entry whose host field matches `host` (ignoring port).
func HasKnownHost(host string) (bool, error) {
	data, err := os.ReadFile(KnownHostsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		for _, h := range strings.Split(fields[0], ",") {
			h = strings.TrimPrefix(h, "[")
			if i := strings.LastIndex(h, "]:"); i >= 0 {
				h = h[:i]
			}
			if h == host {
				return true, nil
			}
		}
	}
	return false, nil
}
