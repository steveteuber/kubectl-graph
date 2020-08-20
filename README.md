# kubectl-graph

[![status](https://img.shields.io/badge/status-WIP-green.svg)](#status)
[![license](https://img.shields.io/github/license/steveteuber/kubectl-graph)](https://github.com/steveteuber/kubectl-graph/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/steveteuber/kubectl-graph)](https://goreportcard.com/report/github.com/steveteuber/kubectl-graph)
![Go Version](https://img.shields.io/github/go-mod/go-version/steveteuber/kubectl-graph)
![Latest Release](https://img.shields.io/github/v/release/steveteuber/kubectl-graph)

A kubectl plugin to visualize Kubernetes resources and relationships.

## Examples

### Grafana Loki

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus.

![Kubernetes resource graph for Grafana Loki](assets/cypher-loki.png)

```
kubectl graph all -n loki -o cypher | cypher-shell -u neo4j -p secret
```

## Status

This `kubectl` plugin is under active development.

## License

This project is licensed under the Apache License 2.0, see [LICENSE](./LICENSE) for more information.
