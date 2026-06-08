//go:build !windows

package transport

import (
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func agentAuth() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers)
}

func AgentStatus() (running bool, keyCount int, detail string) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return false, 0, "SSH_AUTH_SOCK not set"
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false, 0, "socket dial failed: " + err.Error()
	}
	defer conn.Close()
	ag := agent.NewClient(conn)
	keys, err := ag.List()
	if err != nil {
		return true, 0, "connected but List() failed: " + err.Error()
	}
	return true, len(keys), sock
}
