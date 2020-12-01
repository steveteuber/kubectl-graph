# kubectl-graph

[![status](https://img.shields.io/badge/status-WIP-green.svg)](#status)
[![license](https://img.shields.io/github/license/steveteuber/kubectl-graph)](https://github.com/steveteuber/kubectl-graph/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/steveteuber/kubectl-graph)](https://goreportcard.com/report/github.com/steveteuber/kubectl-graph)
[![Workflow Status](https://img.shields.io/github/workflow/status/steveteuber/kubectl-graph/Release)](https://github.com/steveteuber/kubectl-graph/actions?query=workflow:Release)
[![Go Version](https://img.shields.io/github/go-mod/go-version/steveteuber/kubectl-graph)](https://github.com/steveteuber/kubectl-graph/blob/master/go.mod#L3)
[![Latest Release](https://img.shields.io/github/v/release/steveteuber/kubectl-graph)](https://github.com/steveteuber/kubectl-graph/releases/latest)

A kubectl plugin to visualize Kubernetes resources and relationships.

## Quickstart

This quickstart guide uses [homebrew](https://brew.sh) and [docker](https://www.docker.com) on `macOS` in the usage examples.

### Prerequisites

This plugin requires [Graphviz](https://graphviz.org) *or* [Neo4j](https://neo4j.com) to visualize the dependency graph.

#### Graphviz

To render the *default* output format, you'll need to install the `dot` command line tool first:

```
brew install graphviz
```

#### Neo4j

To connect to a Neo4j database, you'll need to install the `cypher-shell` command line tool first:

```
brew install cypher-shell
```

### Installation

This `kubectl` plugin is distributed via [krew](https://krew.sigs.k8s.io).
To install it, run the following command:

before kubectl 1.19
```
kubectl-krew install graph
```

kubectl from 1.19

```
kubectl krew install graph
```

### Usage

#### Graphviz

This `kubectl graph` command will fetch all running Pods in the namespace `kube-system` and outputs a graph in the DOT format.
We could also pipe the output directly to the `dot` command, which can create an image file:

```
kubectl graph pods --field-selector status.phase=Running -n kube-system | dot -T svg -o pods.svg
```

Now we'll have an image file in the current working directory. This SVG file can then be viewed in any web browser:

```
open pods.svg
```

If you're not happy with the SVG output format, please take a look at the offical [Graphviz](https://graphviz.org/doc/info/output.html) documentation.

#### Neo4j

Before you can import all your Kubernetes resources, we need to create a Neo4j database.
I prefer to use the `Neo4j Desktop.app` installed via `brew cask install neo4j`, but you can also start a Neo4j instance via docker:

```
docker run --rm -p 7474:7474 -p 7687:7687 -e NEO4J_AUTH=none neo4j
```

After the container is up and running you can start to import all your Kubernetes resources into Neo4j:

```
kubectl graph all -n kube-system -o cypher | cypher-shell
```

When the import is complete, you can open the Neo4j Browser interface at http://localhost:7474/.

## Examples

### Grafana Loki

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus.

![Kubernetes resource graph for Grafana Loki](assets/cypher-loki.png)

```
kubectl graph all -n loki -o cypher | cypher-shell -u neo4j -p secret
```

## Development

```
go run ./cmd/kubectl-graph/main.go all -n <namspace> | dot -T png -o all.png
```

## Status

This `kubectl` plugin is under active development.

## License

This project is licensed under the Apache License 2.0, see [LICENSE](./LICENSE) for more information.
