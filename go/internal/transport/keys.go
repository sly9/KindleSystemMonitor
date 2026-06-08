package transport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

type KeyCandidate struct {
	Path      string
	Comment   string
	Encrypted bool
	Loaded    bool
	Err       error
}

// DiscoverKeys returns candidates in priority order.
// explicitPath != "" forces a single candidate (and surfaces errors).
// Otherwise we probe well-known names under ~/.ssh and silently skip missing ones.
func DiscoverKeys(explicitPath string) []KeyCandidate {
	var paths []string
	if explicitPath != "" {
		paths = []string{expandHome(explicitPath)}
	} else {
		sshDir := userSSHDir()
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_rsa_kindle"} {
			paths = append(paths, filepath.Join(sshDir, name))
		}
	}

	var out []KeyCandidate
	for _, p := range paths {
		c := KeyCandidate{Path: p}
		data, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && explicitPath == "" {
				continue
			}
			c.Err = err
			out = append(out, c)
			continue
		}
		c.Encrypted = isEncryptedKey(data)
		c.Comment = readPubComment(p + ".pub")
		if !c.Encrypted {
			if _, err := ssh.ParsePrivateKey(data); err != nil {
				c.Err = err
			} else {
				c.Loaded = true
			}
		}
		out = append(out, c)
	}
	return out
}

func isEncryptedKey(data []byte) bool {
	return strings.Contains(string(data), "ENCRYPTED")
}

func readPubComment(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	parts := strings.Fields(string(b))
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

func userSSHDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// buildAuth returns ssh.AuthMethods in priority order:
//   each loadable on-disk key (PublicKeys), then ssh-agent (PublicKeysCallback).
//   Encrypted keys are skipped here — they ride on the agent path.
// rsaSignerWithFallback wraps an RSA signer so the client will offer
// rsa-sha2-256/512 first and fall back to legacy ssh-rsa. Older SSH servers
// (e.g. dropbear on Kindle PW3) only accept a subset; without this the client
// can pick an algorithm the server silently rejects.
func rsaSignerWithFallback(s ssh.Signer) ssh.Signer {
	if s.PublicKey().Type() != ssh.KeyAlgoRSA {
		return s
	}
	algSigner, ok := s.(ssh.AlgorithmSigner)
	if !ok {
		return s
	}
	multi, err := ssh.NewSignerWithAlgorithms(algSigner, []string{
		ssh.KeyAlgoRSASHA512,
		ssh.KeyAlgoRSASHA256,
		ssh.KeyAlgoRSA,
	})
	if err != nil {
		return s
	}
	return multi
}

func buildAuth(explicitPath string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	for _, k := range DiscoverKeys(explicitPath) {
		if !k.Loaded {
			continue
		}
		data, err := os.ReadFile(k.Path)
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		methods = append(methods, ssh.PublicKeys(rsaSignerWithFallback(s)))
	}
	if a := agentAuth(); a != nil {
		methods = append(methods, a)
	}
	return methods, nil
}
