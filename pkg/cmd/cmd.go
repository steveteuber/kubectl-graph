package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/steveteuber/kubectl-graph/pkg/graph"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	// Import to initialize client auth plugins
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

var (
	graphLong = templates.LongDesc(`
		A kubectl plugin to visualize Kubernetes resources and relationships.`)

	graphExample = templates.Examples(`
		# Visualize all pods in graphviz output format.
		%[1]s graph deployments,replicasets,pods | dot -T svg -o pods.svg

		# Visualize all pods in cypher output format.
		%[1]s graph deployments,replicasets,pods -o cypher | cypher-shell -u neo4j -p secret

		# Visualize deployments in cypher output format, in the "v1" version of the "apps" API group:
		%[1]s graph deployments.v1.apps -o cypher

		# Visualize resources from a directory with kustomization.yaml - e.g. dir/kustomization.yaml.
		%[1]s graph -k dir/ | dot -T svg -o kustomization.svg

		# Visualize all pods and networkpolicies together in graphviz output format.
		%[1]s graph networkpolicies | dot -T svg -o networkpolicies.svg`)
)

// GraphOptions contains the input to the graph command.
type GraphOptions struct {
	configFlags *genericclioptions.ConfigFlags

	AllNamespaces     bool
	ChunkSize         int64
	CmdParent         string
	ExplicitNamespace bool
	FieldSelector     string
	LabelSelector     string
	Namespace         string
	OutputFormat      string

	resource.FilenameOptions
	genericclioptions.IOStreams
}

// NewGraphOptions returns a GraphOptions with default chunk size 500.
func NewGraphOptions(parent string, flags *genericclioptions.ConfigFlags, streams genericclioptions.IOStreams) *GraphOptions {
	return &GraphOptions{
		configFlags: flags,
		CmdParent:   parent,
		IOStreams:   streams,
		ChunkSize:   500,
	}
}

// NewCmdGraph creates a command object for the "graph" action.
func NewCmdGraph(parent string, flags *genericclioptions.ConfigFlags, streams genericclioptions.IOStreams) *cobra.Command {
	f := cmdutil.NewFactory(flags)
	o := NewGraphOptions(parent, flags, streams)

	cmd := &cobra.Command{
		Use:                   fmt.Sprintf("%s graph [(-o|--output=)cql|cypher|dot|graphviz] (TYPE[.VERSION][.GROUP] ...) [flags]", parent),
		DisableFlagsInUseLine: true,
		Short:                 "Visualize one or many resources and relationships",
		Long:                  graphLong + "\n\n" + cmdutil.SuggestAPIResources(parent),
		Example:               fmt.Sprintf(graphExample, parent),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate(cmd, args))
			cmdutil.CheckErr(o.Run(f, cmd, args))
		},
	}

	cmd.Flags().BoolP("help", "h", false, fmt.Sprintf("Help for %s graph", parent))
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", o.AllNamespaces, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	cmd.Flags().Int64Var(&o.ChunkSize, "chunk-size", o.ChunkSize, "Return large lists in chunks rather than all at once. Pass 0 to disable.")
	cmd.Flags().StringVar(&o.FieldSelector, "field-selector", o.FieldSelector, "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	cmd.Flags().StringVarP(&o.LabelSelector, "selector", "l", o.LabelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().StringVarP(&o.OutputFormat, "output", "o", o.OutputFormat, "Output format. One of: cql|cypher|dot|graphviz.")
	cmdutil.AddFilenameOptionFlags(cmd, &o.FilenameOptions, "identifying the resource to get from a server.")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// Complete takes the command arguments and factory and infers any remaining options.
func (o *GraphOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error

	o.Namespace, o.ExplicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}
	if o.AllNamespaces {
		o.ExplicitNamespace = false
	}

	switch o.OutputFormat {
	case "cql", "cyp":
		o.OutputFormat = "cypher"
	case "dot", "":
		o.OutputFormat = "graphviz"
	}

	return nil
}

// Validate checks the set of flags provided by the user.
func (o *GraphOptions) Validate(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmdutil.IsFilenameSliceEmpty(o.Filenames, o.Kustomize) {
		return fmt.Errorf("You must specify the type of resource to graph. %s", cmdutil.SuggestAPIResources(o.CmdParent))
	}
	if !(o.OutputFormat == "cypher" || o.OutputFormat == "graphviz") {
		return fmt.Errorf("Invalid output format: %q, allowed formats are: %s", o.OutputFormat, "cql|cyp|cypher|dot|graphviz")
	}

	return nil
}

// Run performs the graph operation.
func (o *GraphOptions) Run(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	r := f.NewBuilder().
		Unstructured().
		NamespaceParam(o.Namespace).DefaultNamespace().AllNamespaces(o.AllNamespaces).
		FilenameParam(o.ExplicitNamespace, &o.FilenameOptions).
		LabelSelectorParam(o.LabelSelector).
		FieldSelectorParam(o.FieldSelector).
		RequestChunksOf(o.ChunkSize).
		ResourceTypeOrNameArgs(true, args...).
		ContinueOnError().
		Latest().
		Flatten().
		Do()

	if err := r.Err(); err != nil {
		return err
	}

	clientset, err := f.KubernetesClientSet()
	if err != nil {
		return err
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	objs := make([]*unstructured.Unstructured, len(infos))
	for ix := range infos {
		objs[ix] = infos[ix].Object.(*unstructured.Unstructured)
	}

	graph, err := graph.NewGraph(clientset, objs)
	if err != nil {
		return err
	}

	return graph.Write(o.Out, o.OutputFormat)
}
