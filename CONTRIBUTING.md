# Contributing

For general Falco contribution guidelines, including DCO sign-off, see the organization-wide [CONTRIBUTING.md](https://github.com/falcosecurity/.github/blob/main/CONTRIBUTING.md).

## Helm Chart Contributions

The k8s-metacollector chart source lives in this repository under [`chart/k8s-metacollector`](chart/k8s-metacollector). This repository is the source of truth for chart changes, chart version bumps, changelog entries, and generated chart docs.

Published Falco charts live in [`falcosecurity/charts`](https://github.com/falcosecurity/charts). After chart changes are merged here, Falco infrastructure opens or updates the matching chart release PR in `falcosecurity/charts`.

PRs that change chart templates, values, release metadata, or the application version rendered by the chart must update [`chart/k8s-metacollector/Chart.yaml`](chart/k8s-metacollector/Chart.yaml) and [`chart/k8s-metacollector/CHANGELOG.md`](chart/k8s-metacollector/CHANGELOG.md) in this repository.

Use SemVer for the chart `version`: major for breaking changes to values, rendered resources, or upgrade behavior; minor for backward-compatible chart features; patch for backward-compatible fixes or metadata changes. Set `appVersion` to the k8s-metacollector version rendered by the chart.

If chart values or chart documentation change, update [`chart/k8s-metacollector/README.gotmpl`](chart/k8s-metacollector/README.gotmpl) and regenerate [`chart/k8s-metacollector/README.md`](chart/k8s-metacollector/README.md).

Before opening a chart PR, run:

```bash
make chart-check
```
