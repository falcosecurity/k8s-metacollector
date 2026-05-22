# Contributing

For general Falco contribution guidelines, including DCO sign-off, see the organization-wide [CONTRIBUTING.md](https://github.com/falcosecurity/.github/blob/main/CONTRIBUTING.md).

## Helm Chart Contributions

The k8s-metacollector chart source lives in [`chart/k8s-metacollector`](chart/k8s-metacollector). Published Falco charts live in [`falcosecurity/charts`](https://github.com/falcosecurity/charts), and Falco infrastructure syncs this chart there only when the chart version is bumped.

Open k8s-metacollector chart issues and PRs in this repository; `falcosecurity/charts` receives the generated sync PR.

- Regular chart PRs: do not bump [`chart/k8s-metacollector/Chart.yaml`](chart/k8s-metacollector/Chart.yaml); add the change under `## Unreleased` in [`chart/k8s-metacollector/CHANGELOG.md`](chart/k8s-metacollector/CHANGELOG.md).
- Chart release PRs: use `/kind chart-release`, bump [`chart/k8s-metacollector/Chart.yaml`](chart/k8s-metacollector/Chart.yaml), and move the selected `## Unreleased` entries into the new version section. Entries not included in that release can stay under `## Unreleased`.

Use SemVer for the chart `version`: major for breaking changes, minor for backward-compatible chart features, patch for fixes or metadata changes. Set `appVersion` to the k8s-metacollector version rendered by the chart when preparing a chart release.

If chart values or chart documentation change, update [`chart/k8s-metacollector/README.gotmpl`](chart/k8s-metacollector/README.gotmpl) and regenerate [`chart/k8s-metacollector/README.md`](chart/k8s-metacollector/README.md).

Before opening a chart PR, run:

```bash
make chart-check
```
