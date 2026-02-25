package runner

import (
	"os/exec"
	"strings"
)

type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
	RunWithStdin(stdin string, name string, args ...string) ([]byte, error)
}

type RealRunner struct{}

func (r *RealRunner) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (r *RealRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (r *RealRunner) RunWithStdin(stdin string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Output()
}
