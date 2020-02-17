package main

import (
	"os"

	"github.com/steveteuber/kubectl-graph/pkg/cmd"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func main() {
	flags := genericclioptions.NewConfigFlags(true)
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}

	command := cmd.NewCmdGraph("kubectl", flags, streams)
	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}
