Helm chart repository for velero-volume-controller
==================================================

Principle
---------

Helm repositories are simple static websites that contains the charts as archives, and an `index.yaml` to reference them.

Using GitHub pages and the ability of the `helm` binary to generate the Helm repository assets, one can simply expose a Helm repository from a GIT repository.

Setup
-----

_To be done only once in the Helm repository life._

1. Enable GitHub pages for the GIT repository, using "Follows the master branch" mode.
2. Create a `charts` folder at the root of the GIT repository.
3. Generate an `index.yaml` using the following commands:

```bash
git checkout master
cd charts
helm repo index . # Create the new index.yaml file in current directory
git commit -a -m 'Initialize the helm repository'
git push
```

The helm repository is ready to be served at the URL `https://[USERNAME].github.io/velero-volume-controller/charts`.

Release a new version of the chart
----------------------------------

_To be done each time a new version of the chart should be released._

1. Prepare the new chart sources, without forgetting to bump the `version` field at `charts/velero-volume-controller/Chart.yaml`.
2. Execute the following commands:

```bash
cd charts
helm package velero-volume-controller # Create the tgz archive for the new version of the chart in current directory
git commit -a -m 'New chart version'
git push
```
