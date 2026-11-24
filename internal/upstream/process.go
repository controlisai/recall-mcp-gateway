package upstream

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Process manages the upstream MCP server subprocess (stdio transport).
type Process struct {
	cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Stdout *bufio.Scanner
}

// StartProcess spawns an upstream MCP server process with the given command and args.
func StartProcess(command string, args []string, env map[string]string) (*Process, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr

	// Merge environment variables
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start upstream %s: %w", command, err)
	}

	return &Process{
		cmd:    cmd,
		Stdin:  stdin,
		Stdout: bufio.NewScanner(stdout),
	}, nil
}

// Write sends a line to the upstream process's stdin.
func (p *Process) Write(data []byte) error {
	_, err := p.Stdin.Write(append(data, '\n'))
	return err
}

// Wait waits for the upstream process to exit.
func (p *Process) Wait() error {
	return p.cmd.Wait()
}

// Kill terminates the upstream process.
func (p *Process) Kill() error {
	return p.cmd.Process.Kill()
}
