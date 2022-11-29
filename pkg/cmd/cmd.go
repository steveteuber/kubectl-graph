package cmd

import (
	"fmt"
	"strings"

	"github.com/schollz/progressbar/v3"
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
	Namespaces        []string
	OutputFormat      string
	Truncate          int

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
		Truncate:    12,
	}
}

// NewCmdGraph creates a command object for the "graph" action.
func NewCmdGraph(parent string, flags *genericclioptions.ConfigFlags, streams genericclioptions.IOStreams) *cobra.Command {
	f := cmdutil.NewFactory(flags)
	o := NewGraphOptions(parent, flags, streams)

	cmd := &cobra.Command{
		Use:                   fmt.Sprintf("%s graph [(-o|--output=)aql|arangodb|cql|cypher|dot|graphviz|mermaid] (TYPE[.VERSION][.GROUP] ...) [flags]", parent),
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
	cmd.Flags().IntVarP(&o.Truncate, "truncate", "t", o.Truncate, "Truncate node name to N characters. This affects graphviz and mermaid output format.")
	cmd.Flags().StringVar(&o.FieldSelector, "field-selector", o.FieldSelector, "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	cmd.Flags().StringVarP(&o.LabelSelector, "selector", "l", o.LabelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().StringVarP(&o.OutputFormat, "output", "o", o.OutputFormat, "Output format. One of: aql|arangodb|cql|cypher|dot|graphviz|mermaid.")
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
	o.Namespaces = strings.Split(o.Namespace, ",")

	if o.AllNamespaces {
		o.ExplicitNamespace = false
	}

	switch o.OutputFormat {
	case "aql":
		o.OutputFormat = "arangodb"
	case "cql":
		o.OutputFormat = "cypher"
	case "dot", "":
		o.OutputFormat = "graphviz"
	}

	return nil
}

// Validate checks the set of flags provided by the user.
func (o *GraphOptions) Validate(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && cmdutil.IsFilenameSliceEmpty(o.Filenames, o.Kustomize) {
		return fmt.Errorf("you must specify the type of resource to graph. %s", cmdutil.SuggestAPIResources(o.CmdParent))
	}
	if !(o.OutputFormat == "arangodb" || o.OutputFormat == "cypher" || o.OutputFormat == "graphviz" || o.OutputFormat == "mermaid") {
		return fmt.Errorf("invalid output format: %q, allowed formats are: %s", o.OutputFormat, "aql|arangodb|cql|cypher|dot|graphviz|mermaid")
	}

	return nil
}

// Run performs the graph operation.
func (o *GraphOptions) Run(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	config, err := f.ToRESTConfig()
	if err != nil {
		return err
	}

	fmt.Fprintf(o.ErrOut, "Please wait while retrieving data from %s\n", config.Host)

	clientset, err := f.KubernetesClientSet()
	if err != nil {
		return err
	}

	objs := []*unstructured.Unstructured{}
	for _, namespace := range o.Namespaces {
		r := f.NewBuilder().
			Unstructured().
			NamespaceParam(namespace).DefaultNamespace().AllNamespaces(o.AllNamespaces).
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

		infos, err := r.Infos()
		if err != nil {
			return err
		}

		for _, info := range infos {
			objs = append(objs, info.Object.(*unstructured.Unstructured))
		}
	}

	bar := progressbar.NewOptions(len(objs),
		progressbar.OptionSetDescription("Processing..."),
		progressbar.OptionSetWriter(o.ErrOut),
		progressbar.OptionSetWidth(10+len(config.Host)),
		progressbar.OptionShowCount(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(o.ErrOut, "\n")
		}),
	)

	graph, err := graph.NewGraph(clientset, objs, func() { bar.Add(1) })
	if err != nil {
		return err
	}

	if o.Truncate > 0 {
		graph.Options.Truncate = o.Truncate
	}

	return graph.Write(o.Out, o.OutputFormat)
}
