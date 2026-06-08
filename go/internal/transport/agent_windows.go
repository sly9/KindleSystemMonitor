//go:build windows

package transport

import (
	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const windowsAgentPipe = `\\.\pipe\openssh-ssh-agent`

func agentAuth() ssh.AuthMethod {
	conn, err := winio.DialPipe(windowsAgentPipe, nil)
	if err != nil {
		return nil
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers)
}

func AgentStatus() (running bool, keyCount int, detail string) {
	conn, err := winio.DialPipe(windowsAgentPipe, nil)
	if err != nil {
		return false, 0, "pipe " + windowsAgentPipe + " not listening"
	}
	defer conn.Close()
	ag := agent.NewClient(conn)
	keys, err := ag.List()
	if err != nil {
		return true, 0, "connected but List() failed: " + err.Error()
	}
	return true, len(keys), windowsAgentPipe
}
